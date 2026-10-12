"""Separate T003 orchestration persistence; no guard or external-write authority.

Hold open_journal for the entire command, including any future external operation.
Only local Linux/macOS filesystems supported by single_task_state are accepted.
SQLite works in bounded memory; committed snapshots are saved through retained
O_NOFOLLOW directory/file descriptors, fsync and atomic relative replacement.
No SQLite absolute-path open can follow a replaced directory into another tree.
Interrupted candidates are retained and never promoted to committed evidence.
An external action may start only AFTER its enclosing intent transaction returns.
Ownership ports are trusted host injections, never loaded from package data. A fresh
exclusive observation is mandatory: local locks and stored observations prove no
cross-host exclusion. Callers supply non-secret, bounded evidence only.
"""
from contextlib import closing, contextmanager
from dataclasses import asdict, dataclass
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import uuid

import single_task_state as durable
from single_task_state import InvalidState, PersistenceFailure, StateConflict
import approval_binding as binding
import task_package as package
import task_tracker as tracker_ports
import task_source


LIMIT = 1024 * 1024
MAX_DATABASE = 16 * LIMIT
SCHEMA = """
CREATE TABLE issue (repository TEXT NOT NULL, issue TEXT NOT NULL);
CREATE TABLE owner (identity BLOB NOT NULL);
CREATE TABLE records (key TEXT PRIMARY KEY, identity BLOB NOT NULL);
CREATE TABLE events (
    key TEXT NOT NULL REFERENCES records(key),
    sequence INTEGER NOT NULL CHECK(sequence > 0),
    kind TEXT NOT NULL,
    payload BLOB NOT NULL,
    digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (key, sequence)
);
PRAGMA user_version = 1;
"""
for _table in ('issue', 'owner', 'records', 'events'):
    for _action in ('update', 'delete'):
        SCHEMA += (f'CREATE TRIGGER immutable_{_table}_{_action} BEFORE {_action.upper()} '
                   f'ON {_table} BEGIN SELECT RAISE(ABORT, "append-only evidence"); END;\n')


def _text(value):
    if (not isinstance(value, str) or not value or len(value) > 1024
            or any(ord(character) < 32 or ord(character) == 127 for character in value)):
        raise InvalidState('Invalid journal identifier')
    try:
        value.encode('utf-8')
    except UnicodeError:
        raise InvalidState('Invalid journal identifier') from None
    return value


def _encode(value):
    encoded = durable._canonical(value)
    if len(encoded) > LIMIT:
        raise InvalidState('Journal evidence limit exceeded')
    return encoded


def _digest(value):
    return hashlib.sha256(value).hexdigest()


@dataclass(frozen=True)
class HandoffKey:
    repository: str
    issue: str
    package_digest: str
    guard_attempt_id: str

    def encoded(self):
        for value in asdict(self).values():
            _text(value)
        if not re.fullmatch('[0-9a-f]{64}', self.package_digest):
            raise InvalidState('Invalid package digest')
        return _encode(asdict(self))


@dataclass(frozen=True)
class Owner:
    checkout: str
    host: str
    process: str

    def encoded(self):
        for value in asdict(self).values():
            _text(value)
        return _encode(asdict(self))


@dataclass(frozen=True)
class OwnershipEvidence:
    repository: str
    issue: str
    owner: Owner
    state: str
    verifier: str
    reference: str


class UnavailableOwnership:
    def observe(self, repository, issue, owner):
        return None


PROGRESS = ('prepared', 'validated-offline', 'ready', 'guard-active',
            'completed-awaiting-handoff', 'reviewed', 'published', 'evidence-durable',
            'final-label-intent', 'handed-off')


@dataclass(frozen=True)
class GuardRecovery:
    """Trusted guard observation, not a second implementation of guard state.

    resume must reconcile its existing attempt and return fresh acceptance/seal/
    commit-binding evidence. No legacy single_task fallback is provided here.
    Both resume/rework accept operation_key: an interrupted intent must reconcile
    that same guard operation without duplicating implementation or a child attempt.
    """
    attempt_id: str
    mode: str
    progress: str
    implementation_commit: str | None
    content_digest: str | None
    verification_head: str
    checks_ref: str
    predecessor_attempt_id: str | None = None


def _matches(value, expression):
    return type(value) is str and re.fullmatch(expression, value) is not None


def _reference(value):
    return _matches(value, r'sha256:[0-9a-f]{64}')


def _guard_call(method, *args, **kwargs):
    try:
        return method(*args, **kwargs)
    except Exception:
        # Guard/backend diagnostics and exception classes are untrusted output;
        # storage errors raised by our own journal still propagate separately.
        raise StateConflict('Guard recovery unavailable; retain evidence') from None


def _observation(value, attempt):
    if (type(value) is not GuardRecovery or value.attempt_id != attempt
            or value.progress not in ('active', 'completed')
            or value.mode not in package._MODES
            or not _matches(value.verification_head, r'[0-9a-f]{40}')
            or not _reference(value.checks_ref)):
        raise InvalidState('Invalid guard recovery evidence')
    identity = value.implementation_commit, value.content_digest
    if identity != (None, None) and not (
            _matches(identity[0], r'[0-9a-f]{40}') and _matches(identity[1], r'[0-9a-f]{64}')):
        raise InvalidState('Invalid guard implementation identity')
    if (value.progress == 'completed' and (None in identity or value.mode != 'handoff-only')
            or value.progress == 'active' and value.mode == 'handoff-only'):
        raise InvalidState('Guard recovery mode conflict')
    return value


class Recovery:
    """Append-only orchestration around host-injected Feature 003 recovery.

    Keep the journal open for this object's entire lifetime. initialize is only
    for a newly prepared package; resume never creates a missing record. Stage
    evidence is supplied by trusted callers, not parsed from issue/package prose.
    T015/T016 own external reconciliation; unknown operations cannot advance.
    """

    def __init__(self, journal, key, owner, ownership=None):
        self.journal, self.key = journal, key
        self.owner, self.ownership = owner, ownership

    def _ownership(self):
        return self.journal.require_ownership(self.owner, self.ownership)

    def _inputs(self, value):
        value = package.build_package(value)
        if (value['repository'] != self.key.repository
                or value['package_digest'] != self.key.package_digest
                or value['issue'] is not None and str(value['issue']['number']) != self.key.issue):
            raise StateConflict('Recovery package identity conflict')
        return dict(revision_digest=package.package_revision_digest(value),
                    package_digest=value['package_digest'], guard_attempt_id=self.key.guard_attempt_id,
                    guard_revision=value['execution']['guard_revision'])

    def initialize(self, value):
        inputs = self._inputs(value)
        self.journal.claim_owner(self.owner)
        self._ownership()
        # One active orchestration identity per issue. Generic T003 records may
        # coexist, but a changed package/attempt cannot invent another baseline.
        identity, _ = self.journal._key(self.key)
        for row in self.journal._db.execute(
                "SELECT DISTINCT key FROM events WHERE kind='lifecycle-inputs'"):
            if row[0] != identity:
                raise StateConflict('Existing issue recovery identity retained')
        with self.journal.transaction():
            self.journal.create_record(self.key)
            events = self.journal.events(self.key)
            if events:
                if events[0]['kind'] != 'lifecycle-inputs' or events[0]['payload'] != inputs:
                    raise StateConflict('Recovery inputs changed; original retained')
                self.status()
                return
            self.journal.append(self.key, 'lifecycle-inputs', inputs)
            self.journal.append(self.key, 'lifecycle-progress', dict(progress='prepared', evidence={}))

    def status(self):
        events = self.journal.events(self.key)
        if not events or events[0]['kind'] != 'lifecycle-inputs':
            raise InvalidState('Recovery record missing; retain evidence')
        current = dict(progress=None, implementation_commit=None, content_digest=None,
                       verification_head=None, checks_ref=None, effective_mode=None,
                       binding_digest=None, review_candidate=None, verification_valid=False,
                       rework_attempt_id=None, rework_implementation_commit=None,
                       rework_content_digest=None, blockers=[])
        for event in events[1:]:
            kind, payload = event['kind'], event['payload']
            if kind == 'lifecycle-progress':
                if (type(payload) is not dict or set(payload) != {'progress', 'evidence'}
                        or payload['progress'] not in PROGRESS):
                    raise InvalidState('Unknown recovery progress retained')
                target = payload['progress']
                index = -1 if current['progress'] is None else PROGRESS.index(current['progress'])
                if PROGRESS.index(target) != index + 1:
                    raise InvalidState('Nonmonotonic recovery progress retained')
                self._stage(target, payload['evidence'], current)
                current['progress'] = target
                if target == 'ready':
                    current['binding_digest'] = payload['evidence']['binding_digest']
                if target == 'reviewed':
                    current['review_candidate'] = payload['evidence']['candidate']
            elif kind == 'lifecycle-rework':
                if (type(payload) is not dict or set(payload) != {
                        'attempt_id', 'predecessor_attempt_id', 'prior_commit', 'prior_content_digest'}
                        or payload['predecessor_attempt_id'] != self.key.guard_attempt_id
                        or not _matches(payload['attempt_id'], r'[A-Za-z0-9_-]{1,128}')
                        or payload['attempt_id'] == self.key.guard_attempt_id
                        or payload['prior_commit'] != current['implementation_commit']
                        or payload['prior_content_digest'] != current['content_digest']
                        or current['rework_attempt_id'] not in (None, payload['attempt_id'])):
                    raise InvalidState('Invalid guard-owned rework link retained')
                current['rework_attempt_id'] = payload['attempt_id']
                current['verification_valid'] = False
            elif kind == 'lifecycle-verification':
                if PROGRESS.index(current['progress']) < PROGRESS.index('guard-active'):
                    raise InvalidState('Guard verification without active attempt retained')
                attempt = current['rework_attempt_id'] or self.key.guard_attempt_id
                observation = _observation(GuardRecovery(**payload), attempt)
                self._identity(observation, current)
                current.update(verification_head=observation.verification_head,
                               checks_ref=observation.checks_ref, effective_mode=observation.mode,
                               verification_valid=observation.progress == 'completed')
                if observation.progress == 'completed':
                    if current['rework_attempt_id']:
                        current.update(rework_implementation_commit=observation.implementation_commit,
                                       rework_content_digest=observation.content_digest)
                    else:
                        current.update(implementation_commit=observation.implementation_commit,
                                       content_digest=observation.content_digest)
            elif kind == 'lifecycle-blocker':
                if type(payload) is not dict or set(payload) != {'code'} or not _matches(
                        payload['code'], r'[a-z][a-z0-9-]{0,80}'):
                    raise InvalidState('Unknown recovery blocker retained')
                current['blockers'].append(payload)
                current['verification_valid'] = False
            elif kind.startswith('lifecycle-'):
                raise InvalidState('Unknown recovery event retained')
        if current['progress'] is None:
            raise InvalidState('Recovery progress missing; retain evidence')
        return current

    def _identity(self, observation, state):
        if state['rework_attempt_id']:
            if (observation.attempt_id != state['rework_attempt_id']
                    or observation.predecessor_attempt_id != self.key.guard_attempt_id):
                raise StateConflict('Guard rework predecessor changed')
            if state['rework_implementation_commit'] is not None and (
                    (observation.implementation_commit, observation.content_digest)
                    != (state['rework_implementation_commit'], state['rework_content_digest'])
                    or observation.progress != 'completed'):
                raise StateConflict('Immutable guard rework completion changed')
        elif observation.predecessor_attempt_id is not None:
            raise StateConflict('Unexpected guard predecessor')
        elif state['implementation_commit'] is not None:
            if ((observation.implementation_commit, observation.content_digest)
                    != (state['implementation_commit'], state['content_digest'])
                    or observation.progress != 'completed'
                    and observation.mode != 'owner-authorized rework'):
                raise StateConflict('Immutable guard completion changed; retain evidence')

    def _stage(self, target, evidence, state):
        if type(evidence) is not dict:
            raise InvalidState('Invalid recovery stage evidence')
        required = {
            'prepared': set(), 'validated-offline': {'evidence_ref'}, 'ready': {'binding_digest'},
            'guard-active': {'guard_attempt_id'}, 'completed-awaiting-handoff': {'checks_ref'},
            'reviewed': {'candidate', 'evidence_ref'},
            'published': {'operation_key'}, 'evidence-durable': {'operation_key'},
            'final-label-intent': {'operation_key'}, 'handed-off': {'operation_key'},
        }[target]
        if set(evidence) != required:
            raise InvalidState('Missing recovery stage evidence')
        if 'evidence_ref' in evidence and not _reference(evidence['evidence_ref']):
            raise InvalidState('Invalid recovery evidence reference')
        if target == 'ready' and not _matches(evidence['binding_digest'], r'[0-9a-f]{64}'):
            raise InvalidState('Invalid readiness binding')
        if target == 'guard-active' and evidence['guard_attempt_id'] != self.key.guard_attempt_id:
            raise StateConflict('Guard attempt changed')
        if target == 'completed-awaiting-handoff' and (
                state['implementation_commit'] is None or evidence['checks_ref'] != state['checks_ref']):
            raise StateConflict('Guard completion evidence required')
        if (PROGRESS.index(target) >= PROGRESS.index('completed-awaiting-handoff')
                and not state['verification_valid']):
            raise StateConflict('Fresh successful verification required after blocker')
        if target == 'reviewed' and evidence['candidate'] != state['verification_head']:
            raise StateConflict('Exact verification candidate review required')
        if (PROGRESS.index(target) > PROGRESS.index('reviewed')
                and state['review_candidate'] != state['verification_head']):
            raise StateConflict('Exact candidate review is stale; retain completed implementation')
        if 'operation_key' in evidence:
            operation = self.journal.operation(self.key, evidence['operation_key'])
            if (operation is None or target != 'final-label-intent'
                    and operation['status'] != 'succeeded'):
                raise StateConflict('Durable operation confirmation required')

    def advance(self, target, evidence):
        self._ownership()
        state = self.status()
        previous = [event for event in self.journal.events(self.key)
                    if event['kind'] == 'lifecycle-progress'
                    and event['payload']['progress'] == target]
        if previous:
            if previous[0]['payload']['evidence'] != evidence:
                raise StateConflict('Changed recovery stage intent retained')
            return state
        if target not in PROGRESS or PROGRESS.index(target) != PROGRESS.index(state['progress']) + 1:
            raise StateConflict('Recovery progress cannot skip or reopen work')
        self._stage(target, evidence, state)
        self.journal.append(self.key, 'lifecycle-progress', dict(progress=target, evidence=evidence))
        return self.status()

    def _ready(self, readiness, boundary, state):
        result = _guard_call(readiness, boundary=boundary) if callable(readiness) else None
        if (type(result) is not dict or result.get('readiness_valid') is not True
                or result.get('blockers') != []
                or result.get('binding_digest') != state['binding_digest']):
            raise StateConflict('Fresh bound readiness required')

    def _corrective(self, value, corrective, authority, state):
        if (type(corrective) is not dict or set(corrective) != {
                'prior_commit', 'prior_content_digest', 'scope', 'checks', 'approval_ref'}
                or state['implementation_commit'] is None
                or corrective['prior_commit'] != state['implementation_commit']
                or corrective['prior_content_digest'] != state['content_digest']
                or type(corrective['scope']) is not list or not corrective['scope']
                or len(corrective['scope']) > 1000 or len(set(corrective['scope'])) != len(corrective['scope'])
                or type(corrective['checks']) is not list or not corrective['checks']
                or len(corrective['checks']) > 100 or not corrective['approval_ref']
                or not package._reference(corrective['approval_ref'])):
            raise StateConflict('Explicit corrective scope and checks required')
        for path in corrective['scope']:
            package._path_parts(path)
        for argv in corrective['checks']:
            if (type(argv) is not list or not argv or len(argv) > 100
                    or any(not isinstance(arg, str) or not arg or '\x00' in arg for arg in argv)):
                raise StateConflict('Explicit corrective check argv required')
        target = binding.ApprovalTarget('rework', state['implementation_commit'],
            self.key.package_digest, _digest(_encode(corrective)))
        verification = authority.verify(corrective['approval_ref'], target) if authority else None
        if (type(verification) is not binding.ApprovalVerification
                or verification.approved is not True or verification.assurance != 'authenticated-provider'
                or verification.target != target or not verification.evidence_ref
                or not package._reference(verification.evidence_ref)):
            raise StateConflict('Authenticated corrective owner approval required')
        return verification

    def resume(self, value, snapshot, evidence, *, guard=None, readiness=None, live=False,
               corrective=None, authority=None):
        """Reconcile one existing attempt; never begin/reset/check a task here.

        readiness is a trusted host closure over T013's complete entry/post-check
        revalidation. Guard ports own scope, acceptance, recovery and rework. All
        exceptions become fixed blockers; backend diagnostics are never retained.
        """
        state = self.status()  # Missing/corrupt records cannot safely be appended.
        result = dict(state, resume_valid=False, execution_eligible=False,
                      authenticated_approval=None, independent_review=None, live_execution=None,
                      blockers=[])
        code = 'recovery-unavailable'
        try:
            code = 'recovery-input-conflict'
            if self._inputs(value) != self.journal.events(self.key)[0]['payload']:
                raise StateConflict('Recovery inputs changed')
            if (type(snapshot) is not dict or not _matches(snapshot.get('head'), r'[0-9a-f]{40}')
                    or str(Path(snapshot['root']).resolve()) != self.journal.root
                    or live and value['issue'] is None):
                raise StateConflict('Exact recovery source identity required')
            code = 'recovery-ownership-conflict'
            self._ownership()
            code = 'recovery-progress-blocked'
            if PROGRESS.index(state['progress']) < PROGRESS.index('ready'):
                raise StateConflict('Existing ready attempt required')
            mode = 'owner-authorized rework' if corrective is not None else value['mode']
            code = 'recovery-capability-unavailable'
            for action in ('resume', 'completion'):
                assessment = package.assess_capabilities(action, mode, guard=guard, live=live)
                if (not assessment['capabilities_satisfied'] or guard.revision != value['execution']['guard_revision']
                        or any(item['implementation_revision'] != guard.revision
                               for item in assessment['evidence'])):
                    raise StateConflict('Demonstrated guard recovery required')
            code = 'recovery-readiness-blocked'
            self._ready(readiness, 'entry', state)
            code = 'recovery-revalidation-failed'
            verdicts = [_guard_call(guard.validate_scope, snapshot, value['context'], evidence)]
            if corrective is None:
                verdicts.append(_guard_call(guard.validate_acceptance, value['context'], evidence))
            # Corrective entry is validated by guard.rework against the exact
            # authenticated corrective scope/checks and predecessor below. A
            # failed old acceptance is never itself rework authorization.
            for verdict in verdicts:
                if not package._guard_verdict(verdict).accepted:
                    raise StateConflict('Fresh guard verification required')
            self._ready(readiness, 'post-check', state)
            code = 'recovery-ownership-conflict'
            self._ownership()
            if state['progress'] == 'ready':
                self.advance('guard-active', {'guard_attempt_id': self.key.guard_attempt_id})
            code = 'recovery-evidence-invalid'
            attempt = state['rework_attempt_id'] or self.key.guard_attempt_id
            if corrective is None:
                operation_key = 'guard-resume:' + _digest(_encode(dict(
                    inputs=self._inputs(value), attempt=attempt, head=snapshot['head'])))
                self.journal.intent(self.key, operation_key, dict(
                    attempt=attempt, verification_head=snapshot['head'],
                    revision_digest=package.package_revision_digest(value)))
                observation = _guard_call(guard.resume, value['context'], attempt, evidence,
                                           operation_key=operation_key)
            else:
                code = 'recovery-corrective-approval-required'
                authorization = self._corrective(value, corrective, authority, state)
                operation_key = 'guard-rework:' + _digest(_encode(corrective))
                self.journal.intent(self.key, operation_key, corrective)
                previous = self.journal.operation(self.key, operation_key)
                if previous['status'] == 'succeeded':
                    observation = GuardRecovery(**previous['result'])
                else:
                    observation = _guard_call(guard.rework, value['context'], self.key.guard_attempt_id,
                        corrective, authorization, operation_key=operation_key)
            try:
                if corrective is not None:
                    if (type(observation) is not GuardRecovery
                            or observation.attempt_id == self.key.guard_attempt_id
                            or observation.predecessor_attempt_id != self.key.guard_attempt_id
                            or state['rework_attempt_id'] not in (None, observation.attempt_id)):
                        raise StateConflict('New linked guard-owned corrective attempt required')
                    attempt = observation.attempt_id
                observation = _observation(observation, attempt)
            except InvalidState:
                raise StateConflict('Invalid guard recovery evidence') from None
            if (corrective is not None and observation.progress == 'active'
                    and observation.mode != 'owner-authorized rework'):
                raise StateConflict('Guard rework evidence required')
            if corrective is None or state['rework_attempt_id']:
                self._identity(observation, state)
            if observation.verification_head != snapshot['head']:
                raise StateConflict('Guard verification HEAD changed')
            self._ready(readiness, 'post-check', state)
            self._ownership()
            with self.journal.transaction():
                if corrective is not None and state['rework_attempt_id'] is None:
                    self.journal.append(self.key, 'lifecycle-rework', dict(
                        attempt_id=observation.attempt_id, predecessor_attempt_id=self.key.guard_attempt_id,
                        prior_commit=state['implementation_commit'], prior_content_digest=state['content_digest']))
                self.journal.append(self.key, 'lifecycle-verification', asdict(observation))
                if corrective is not None:
                    self.journal.result(self.key, operation_key, 'succeeded', asdict(observation))
                elif observation.progress == 'completed':
                    self.journal.result(self.key, operation_key, 'succeeded', dict(
                        implementation_commit=observation.implementation_commit,
                        content_digest=observation.content_digest))
                if (observation.progress == 'completed'
                        and self.status()['progress'] == 'guard-active'):
                    self.advance('completed-awaiting-handoff', {'checks_ref': observation.checks_ref})
            result.update(self.status(), resume_valid=True, blockers=[])
        except (PersistenceFailure, InvalidState):
            # The storage layer may be unusable; never replace or repair it.
            raise
        except Exception:
            self.journal.append(self.key, 'lifecycle-blocker', {'code': code})
            result.update(self.status(), blockers=[{'code': code}])
        return result


@dataclass(frozen=True)
class IndependentReview:
    """Authenticated host verifier result; review must identify a distinct actor."""
    target: binding.ApprovalTarget
    author_id: str
    reviewer_id: str
    evidence_ref: str
    verifier_revision: str
    assurance: str


def _publication_operation(recovery, tracker, kind, request, before_write):
    """Reconcile authoritative exact state before every bounded write attempt."""
    journal, key = recovery.journal, recovery.key
    operation_key = ('publish-pr:' if kind == 'pr' else 'write-evidence:') + tracker_ports.publication_identity(
        request['identity'])
    intent = {name: request[name] for name in
              ('identity', 'base', 'head', 'checker', 'base_ref', 'head_ref', 'head_repository')}
    intent['content_digest'] = _digest(package.canonical_json(request['content']))
    journal.intent(key, operation_key, intent)
    find = tracker.find_pr if kind == 'pr' else tracker.find_evidence
    write = tracker.publish_pr if kind == 'pr' else tracker.write_evidence
    while True:
        recovery._ownership()
        previous = journal.operation(key, operation_key)
        if previous['status'] == 'ambiguous':
            raise StateConflict('Ambiguous publication retained; explicit operator reconciliation required')
        try:
            found = find(request)
        except tracker_ports.TrackerBlocked as error:
            if str(error) in ('ambiguous-publication-match', 'conflicting-publication-match'):
                journal.result(key, operation_key, 'ambiguous', {'reconcile': 'operator-required'})
            raise StateConflict('Remote publication reconciliation blocked') from None
        except Exception:
            raise StateConflict('Remote publication lookup unavailable') from None
        previous = journal.operation(key, operation_key)
        if found is not None:
            if (type(found) is not tracker_ports.PublicationObject
                    or found.content_digest != intent['content_digest']):
                raise StateConflict('Conflicting remote publication retained')
            journal.result(key, operation_key, 'succeeded', asdict(found))
            return operation_key, found
        if previous['status'] == 'succeeded':
            raise StateConflict('Recorded publication disappeared; retain evidence')
        attempts = sum(event['kind'] == 'publication-attempt'
                       and event['payload']['operation_key'] == operation_key
                       for event in journal.events(key))
        if attempts >= 4:  # Initial write plus at most three reconciled retries.
            raise StateConflict('Publication retry limit reached; operator reconciliation required')
        if previous['status'] in ('intent', 'write-unknown') and attempts:
            journal.result(key, operation_key, 'safe-retry', {'authoritative_absence': True})
        before_write()
        recovery._ownership()
        journal.append(key, 'publication-attempt', dict(operation_key=operation_key, attempt=attempts + 1))
        try:
            write(request, operation_key=operation_key)
        except tracker_ports.WriteUnknown:
            journal.result(key, operation_key, 'write-unknown', {'reconcile': 'exact-identity-content'})
        else:
            # Even a returned write receipt is uncertain until authenticated lookup.
            journal.result(key, operation_key, 'write-unknown', {'reconcile': 'exact-identity-content'})


def publish(recovery, value, snapshot, evidence, *, guard=None, readiness=None, tracker=None,
            authority=None, reviewer=None, implementer_id=None, review_ref=None, publication_ref=None,
            validated_source=None, preparation=None, reviewed_checker=None, observe_refs=None,
            pr_content=None, evidence_content=None, live=False):
    """Freshly verify one existing completion, then reconcile PR and evidence.

    No provider or adapter is selected automatically. Preparation uses the accepted
    checker helper; its pin identifies exact code and grants no owner authority.
    The caller supplies separately authenticated review/publication ports. Final
    labels are a later operation, never part of this function.
    """
    result = dict(recovery.status(), publication_valid=False, execution_eligible=False,
                  authenticated_approval=None, independent_review=None, live_execution=None,
                  pr=None, published_evidence=None, blockers=[])
    code = 'publication-revalidation-blocked'
    try:
        resumed = recovery.resume(value, snapshot, evidence, guard=guard, readiness=readiness, live=live)
        if not resumed['resume_valid'] or not resumed['verification_valid']:
            result.update(resumed, publication_valid=False)
            return result
        code = 'publication-capability-unavailable'
        for name in ('tracker.pr-publication', 'tracker.evidence'):
            _, blocker = package._capability_evidence(name, getattr(tracker, 'capabilities', {}).get(name), live)
            if blocker:
                raise StateConflict('Demonstrated publication capability required')
        code = 'publication-preparation-blocked'
        if (not _matches(reviewed_checker, r'[0-9a-f]{40}') or type(preparation) is not dict
                or preparation.get('source') != reviewed_checker
                or preparation.get('head') != resumed['verification_head']
                or preparation.get('base_ref') != 'refs/heads/main'):
            raise StateConflict('Exact reviewed checker and source pins required')
        prepared = task_source.prepare_candidate(validated_source, preparation)
        if (type(prepared) is not dict or prepared.get('status') != 'PREPARED'
                or prepared.get('check_exit') != 0
                or any(prepared.get(name) != preparation.get(name) for name in ('source', 'base', 'head'))
                or not _matches(prepared.get('tree'), r'[0-9a-f]{40}')):
            raise StateConflict('Checked pinned prepublication preparation required')
        identity = dict(repository=value['repository'], issue=recovery.key.issue, feature=value['feature'],
            task_id=value['task_id'], package_digest=value['package_digest'],
            guard_attempt_id=resumed['rework_attempt_id'] or recovery.key.guard_attempt_id,
            implementation_commit=resumed['rework_implementation_commit'] or resumed['implementation_commit'],
            content_digest=resumed['rework_content_digest'] or resumed['content_digest'])
        tracker_ports.publication_identity(identity)
        requests = {kind: dict(identity=identity, base=prepared['base'], head=prepared['head'],
                    checker=reviewed_checker, base_ref=preparation['base_ref'],
                    head_ref=preparation['head_ref'], head_repository=value['repository'], content=content)
                    for kind, content in (('pr', pr_content), ('evidence', evidence_content))}
        if any(type(request['content']) is not dict for request in requests.values()):
            raise StateConflict('Explicit publication content required')
        binding_digest = _digest(package.canonical_json(dict(requests=requests,
            preparation_tree=prepared['tree'], checks_ref=resumed['checks_ref'])))
        code = 'publication-review-or-authorization-blocked'
        review_target = binding.ApprovalTarget('review', prepared['head'], value['package_digest'], binding_digest)
        publication_target = binding.ApprovalTarget('publication', prepared['head'],
                                                   value['package_digest'], binding_digest)
        if any(not reference or not package._reference(reference) for reference in (review_ref, publication_ref)):
            raise StateConflict('Explicit review and publication references required')

        def proofs():
            review = _guard_call(reviewer.verify, review_ref, review_target) if reviewer else None
            permission = _guard_call(authority.verify, publication_ref, publication_target) if authority else None
            if (type(review) is not IndependentReview or review.target != review_target
                    or review.assurance != 'authenticated-provider' or review.author_id != implementer_id
                    or not tracker_ports._id(review.author_id) or not tracker_ports._id(review.reviewer_id)
                    or review.author_id == review.reviewer_id or review.evidence_ref != review_ref
                    or not _matches(review.verifier_revision, r'[0-9a-f]{40}')
                    or type(permission) is not binding.ApprovalVerification
                    or permission.approved is not True or permission.assurance != 'authenticated-provider'
                    or permission.target != publication_target or not permission.evidence_ref
                    or not package._reference(permission.evidence_ref)):
                raise StateConflict('Fresh independent review and owner publication permission required')
            return review, permission

        review, permission = proofs()
        if not {'publish-pr', 'write-evidence'} <= set(value['execution']['external_actions']):
            raise StateConflict('Package publication actions absent')
        proof = dict(review=asdict(review), permission=asdict(permission), binding_digest=binding_digest)
        recovery.journal.append(recovery.key, 'publication-proof', proof)
        recovery.advance('reviewed', dict(candidate=prepared['head'], evidence_ref='sha256:' + _digest(_encode(proof))))

        def before_write():
            nonlocal code
            code = 'publication-fresh-proof-blocked'
            recovery._ready(readiness, 'post-check', recovery.status())
            proofs()
            if not callable(observe_refs) or _guard_call(observe_refs) != {
                    'base': prepared['base'], 'head': prepared['head']}:
                raise StateConflict('Publication refs moved; retain evidence')

        for kind, target in (('pr', 'published'), ('evidence', 'evidence-durable')):
            code = 'publication-remote-reconciliation-blocked'
            operation_key, found = _publication_operation(recovery, tracker, kind, requests[kind], before_write)
            recovery.advance(target, {'operation_key': operation_key})
            result['pr' if kind == 'pr' else 'published_evidence'] = asdict(found)
        result.update(recovery.status(), publication_valid=True, blockers=[],
                      independent_review=review.evidence_ref, authenticated_approval=permission.evidence_ref)
    except (PersistenceFailure, InvalidState):
        raise
    except Exception:
        recovery.journal.append(recovery.key, 'lifecycle-blocker', {'code': code})
        result.update(recovery.status(), blockers=[{'code': code}])
    return result


def final_labels(recovery, value, *, tracker=None, readiness=None, authorized_actions=(),
                 target='human-review', blocker=None, live=False):
    """Persist recovery, then make one final conditional label operation.

    A returned authenticated receipt is the confirmation; no later provider read,
    publication, check or ownership request is required. Lost responses stop this
    call as incomplete. A later operator/resume call may reconcile the exact set
    before any bounded retry. The journal/checkout are never reset or recreated.
    """
    state = recovery.status()
    result = dict(state, handoff_complete=False, label_transition_confirmed=False,
                  execution_eligible=False, live_execution=None, blockers=[])
    code = 'final-label-prerequisite-blocked'
    try:
        if recovery._inputs(value) != recovery.journal.events(recovery.key)[0]['payload']:
            raise StateConflict('Final handoff inputs changed')
        if state['progress'] == 'handed-off' and target == 'human-review':
            result.update(handoff_complete=True, label_transition_confirmed=True)
            return result
        recovery._ownership()
        if target not in ('human-review', 'blocked'):
            raise StateConflict('Invalid final label target')
        if target == 'blocked':
            if not _matches(blocker, r'[a-z][a-z0-9-]{0,80}'):
                raise StateConflict('Durable safe blocker required')
            recovery.journal.append(recovery.key, 'lifecycle-blocker', {'code': blocker})
            evidence_digest = _digest(_encode(dict(inputs=recovery._inputs(value), blocker=blocker)))
        else:
            if (state['progress'] not in ('evidence-durable', 'final-label-intent', 'handed-off')
                    or not state['verification_valid']
                    or state['review_candidate'] != state['verification_head']):
                raise StateConflict('Durable verified reviewed publication/evidence required')
            event = next(event for event in recovery.journal.events(recovery.key)
                         if event['kind'] == 'lifecycle-progress'
                         and event['payload']['progress'] == 'evidence-durable')
            durable_evidence = recovery.journal.operation(recovery.key,
                event['payload']['evidence']['operation_key'])
            if durable_evidence['status'] != 'succeeded':
                raise StateConflict('Durable evidence confirmation required')
            evidence_digest = _digest(_encode(dict(evidence=durable_evidence['result'],
                implementation=state['rework_implementation_commit'] or state['implementation_commit'],
                verification_head=state['verification_head'], checks_ref=state['checks_ref'],
                review_candidate=state['review_candidate'])))
        code = 'final-label-capability-unavailable'
        _, missing = package._capability_evidence('tracker.safe-labels',
            getattr(tracker, 'capabilities', {}).get('tracker.safe-labels'), live)
        if missing:
            raise StateConflict('Demonstrated conditional label capability required')
        if ('replace-labels' not in authorized_actions
                or 'replace-labels' not in value['execution']['external_actions']):
            raise StateConflict('Final label action not authorized')
        identity = tracker_ports.IssueIdentity(recovery.key.repository, int(recovery.key.issue))
        journal, key = recovery.journal, recovery.key
        code = 'final-label-reconciliation-incomplete'

        def confirmed(operation_key, observation):
            journal.result(key, operation_key, 'succeeded', dict(
                object_digest=observation.object_digest, labels=list(observation.labels), state='open'))
            if target == 'human-review' and recovery.status()['progress'] == 'final-label-intent':
                evidence = {'operation_key': operation_key}
                recovery._stage('handed-off', evidence, recovery.status())
                journal.append(key, 'lifecycle-progress', dict(progress='handed-off', evidence=evidence))
            result.update(recovery.status(), label_transition_confirmed=True,
                          handoff_complete=target == 'human-review', blockers=[])
            return result

        for _ in range(4):
            recovery._ownership()
            observation = _guard_call(tracker.read_issue, identity)
            if (type(observation) is not tracker_ports.IssueObservation
                    or observation.repository != identity.repository or observation.number != identity.number
                    or observation.state != 'open'):
                raise StateConflict('Exact open issue observation required')
            pending = [event['payload'] for event in journal.events(key) if event['kind'] == 'intent'
                       and event['payload']['operation_key'].startswith('final-labels:')
                       and event['payload']['content'].get('evidence_digest') == evidence_digest
                       and event['payload']['content'].get('target') == target]
            for intent in pending:
                operation = journal.operation(key, intent['operation_key'])
                if operation['status'] == 'ambiguous':
                    raise StateConflict('Ambiguous final labels retained')
                if (operation['status'] == 'write-unknown'
                        and set(observation.labels) != set(intent['content']['desired'])
                        and observation.object_digest != intent['content']['expected']['object_digest']):
                    journal.result(key, intent['operation_key'], 'ambiguous', {'recovery': 'operator-required'})
                    raise StateConflict('Uncertain final label state changed; operator reconciliation required')
            if 'workharbor-ready' not in observation.labels:
                for intent in reversed(pending):
                    operation_key = intent['operation_key']
                    operation = journal.operation(key, operation_key)
                    if operation['status'] == 'ambiguous':
                        raise StateConflict('Ambiguous final labels retained')
                    attempted = any(event['kind'] == 'final-label-attempt'
                                    and event['payload']['operation_key'] == operation_key
                                    for event in journal.events(key))
                    if attempted and set(observation.labels) == set(intent['content']['desired']):
                        return confirmed(operation_key, observation)
                raise StateConflict('Final label state unknown; operator reconciliation required')
            desired = tuple(sorted((set(observation.labels) - {'workharbor-ready'}) | {target}))
            expected = dict(object_digest=observation.object_digest, labels=list(observation.labels))
            content = dict(identity=asdict(identity), expected=expected, desired=list(desired),
                evidence_digest=evidence_digest, target=target,
                recovery='retain-checkout-journal; query-exact-label-set-before-retry; owner-reconciles-ambiguity')
            if target == 'blocked':
                content['blocker'] = blocker
            operation_key = 'final-labels:' + _digest(_encode(content))
            journal.intent(key, operation_key, content)
            previous = journal.operation(key, operation_key)
            if previous['status'] in ('succeeded', 'ambiguous'):
                raise StateConflict('Terminal final label result changed remotely')
            attempts = sum(event['kind'] == 'final-label-attempt'
                           and event['payload']['evidence_digest'] == evidence_digest
                           and event['payload']['target'] == target for event in journal.events(key))
            if attempts >= 4:
                raise StateConflict('Final label retry limit retained')
            if previous['status'] == 'write-unknown':
                if observation.object_digest != expected['object_digest']:
                    raise StateConflict('Uncertain label state changed')
                journal.result(key, operation_key, 'safe-retry', {'exact-original-state': True})
            if target == 'human-review' and recovery.status()['progress'] == 'evidence-durable':
                recovery.advance('final-label-intent', {'operation_key': operation_key})
            if target == 'human-review':
                recovery._ready(readiness, 'post-check', recovery.status())
            recovery._ownership()
            journal.append(key, 'final-label-attempt', dict(operation_key=operation_key,
                evidence_digest=evidence_digest, target=target, attempt=attempts + 1))
            try:
                receipt = tracker.replace_labels(identity, dict(
                    object_digest=expected['object_digest'], labels=tuple(expected['labels'])), desired,
                    operation_key=operation_key)
            except tracker_ports.LabelConflict:
                journal.result(key, operation_key, 'safe-retry', {'conditional-conflict': True})
                continue
            except Exception:
                journal.result(key, operation_key, 'write-unknown', {'recovery': content['recovery']})
                break  # No required subsequent provider/agent action after removal.
            if (type(receipt) is not tracker_ports.IssueObservation or receipt.state != 'open'
                    or receipt.repository != identity.repository or receipt.number != identity.number
                    or set(receipt.labels) != set(desired)):
                journal.result(key, operation_key, 'write-unknown', {'recovery': content['recovery']})
                break
            return confirmed(operation_key, receipt)
        raise StateConflict('Final label write incomplete; operator recovery retained')
    except (PersistenceFailure, InvalidState):
        raise
    except Exception:
        recovery.journal.append(recovery.key, 'label-blocker', {'code': code,
            'recovery': 'retain-evidence-and-reconcile-exact-state-before-any-retry'})
        result.update(recovery.status(), blockers=[{'code': code}])
    return result


def handoff(recovery, value, snapshot, evidence, *, publication_ports=None, label_ports=None, live=False):
    """Complete publication/evidence, or reconcile the retained final operation first."""
    state = recovery.status()
    if state['progress'] == 'handed-off':
        if recovery._inputs(value) != recovery.journal.events(recovery.key)[0]['payload']:
            raise StateConflict('Completed handoff inputs changed')
        return dict(state, handoff_complete=True, label_transition_confirmed=True,
                    execution_eligible=False, live_execution=None, blockers=[])
    ports = dict(label_ports or {})
    final_intents = [event['payload']['content'] for event in recovery.journal.events(recovery.key)
                     if event['kind'] == 'intent'
                     and event['payload']['operation_key'].startswith('final-labels:')]
    if final_intents:
        # A final write may already have removed eligibility. Reconcile its durable
        # identity before any new guard/check/publication work can change that proof.
        intent = final_intents[-1]
        return final_labels(recovery, value, target=intent['target'],
                            blocker=intent.get('blocker'), live=live, **ports)
    published = publish(recovery, value, snapshot, evidence, live=live, **dict(publication_ports or {}))
    if not published['publication_valid']:
        blocker = published['blockers'][0]['code'] if published['blockers'] else 'publication-incomplete'
        labels = final_labels(recovery, value, target='blocked', blocker=blocker, live=live, **ports)
        published.update(handoff_complete=False,
                         label_transition_confirmed=labels['label_transition_confirmed'])
        published['blockers'] += labels['blockers']
        return published
    labels = final_labels(recovery, value, live=live, **ports)
    labels.update(pr=published['pr'], published_evidence=published['published_evidence'],
                  independent_review=published['independent_review'],
                  authenticated_approval=published['authenticated_approval'])
    return labels


def _schema(connection):
    return [tuple(row) for row in connection.execute(
        "SELECT type,name,tbl_name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' "
        'ORDER BY type,name')]


class Journal:
    def __init__(self, connection, directory, root, repository, issue):
        self._db = connection
        self._directory = directory
        self._depth = 0
        self.root = str(root)
        self.repository = repository
        self.issue = issue
        self._failed = False
        self._file_identity = self._identity('journal.sqlite3')

    def _identity(self, name):
        try:
            info = os.stat(name, dir_fd=self._directory, follow_symlinks=False)
        except FileNotFoundError:
            return None
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise InvalidState('Unsafe journal file retained')
        return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns

    def _available(self):
        if self._failed:
            raise PersistenceFailure('Persistence uncertain; close and reopen retained journal')

    def _location(self):
        path = Path(self.root) / '.symphony/task-handoff' / _digest(
            _encode([self.repository, self.issue]))
        current = path.lstat()
        pinned = os.fstat(self._directory)
        if (not stat.S_ISDIR(current.st_mode)
                or (current.st_dev, current.st_ino) != (pinned.st_dev, pinned.st_ino)
                or path.resolve() != path):
            raise InvalidState('Journal directory identity changed; retain evidence')

    def _persist(self):
        self._location()
        if self._identity('journal.sqlite3') != self._file_identity:
            raise InvalidState('Committed journal identity changed; retain evidence')
        snapshot = self._db.serialize()
        if len(snapshot) > MAX_DATABASE:
            raise PersistenceFailure('Journal growth limit reached; retain evidence')
        candidate = 'candidate-' + uuid.uuid4().hex + '.sqlite3'
        fd = os.open(candidate, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                     | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=self._directory)
        try:
            with os.fdopen(fd, 'wb', closefd=False) as stream:
                stream.write(snapshot)
                stream.flush()
                os.fsync(fd)
            info = os.fstat(fd)
            identity = info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns
            if self._identity(candidate) != identity:
                raise InvalidState('Checkpoint identity changed; retain evidence')
            os.fsync(self._directory)
            self._location()
            if self._identity('journal.sqlite3') != self._file_identity:
                raise InvalidState('Committed journal identity changed; retain evidence')
            os.replace(candidate, 'journal.sqlite3', src_dir_fd=self._directory,
                       dst_dir_fd=self._directory)
            os.fsync(self._directory)
            if self._identity('journal.sqlite3') != identity:
                raise InvalidState('Checkpoint replacement changed; retain evidence')
            self._location()
            self._file_identity = identity
        finally:
            os.close(fd)

    @contextmanager
    def transaction(self):
        self._available()
        depth = self._depth
        savepoint = 'handoff_' + str(depth)
        prior = self._db.serialize() if depth == 0 else None
        committed = False
        try:
            self._db.execute('BEGIN IMMEDIATE' if depth == 0 else 'SAVEPOINT ' + savepoint)
            self._depth += 1
            try:
                yield self
                self._db.execute('COMMIT' if depth == 0 else 'RELEASE ' + savepoint)
                if depth == 0:
                    committed = True
                    self._persist()
            except BaseException:
                if self._db.in_transaction:
                    if depth == 0:
                        self._db.execute('ROLLBACK')
                    else:
                        self._db.execute('ROLLBACK TO ' + savepoint)
                        self._db.execute('RELEASE ' + savepoint)
                if committed:
                    # The disk may contain old or new state. Restore memory and
                    # prohibit continued calls until a fresh open validates disk.
                    self._db.deserialize(prior)
                    self._failed = True
                raise
            finally:
                self._depth = depth
        except (sqlite3.Error, OSError) as error:
            raise PersistenceFailure('Journal transaction failed; retain evidence') from error

    def _key(self, key):
        self._available()
        if not isinstance(key, HandoffKey):
            raise InvalidState('Invalid handoff key')
        encoded = key.encoded()
        if (key.repository, key.issue) != (self.repository, self.issue):
            raise InvalidState('Handoff key belongs to another issue')
        return _digest(encoded), encoded

    def create_record(self, key):
        identity, encoded = self._key(key)
        with self.transaction():
            row = self._db.execute('SELECT identity FROM records WHERE key=?', (identity,)).fetchone()
            if row is not None:
                if row[0] != encoded:
                    raise StateConflict('Handoff identity conflict; retain original evidence')
                return
            self._db.execute('INSERT INTO records VALUES (?,?)', (identity, encoded))

    def events(self, key):
        identity, encoded = self._key(key)
        row = self._db.execute('SELECT identity FROM records WHERE key=?', (identity,)).fetchone()
        if row is None or row[0] != encoded:
            raise InvalidState('Handoff record missing; retain evidence for recovery')
        return [dict(sequence=row['sequence'], kind=row['kind'], payload=json.loads(row['payload']),
                     digest=row['digest'], created_at=row['created_at'])
                for row in self._db.execute('SELECT * FROM events WHERE key=? ORDER BY sequence',
                                            (identity,))]

    def append(self, key, kind, payload):
        if kind in ('intent', 'result'):
            raise InvalidState('Operation events require the intent/result API')
        return self._append(key, kind, payload)

    def _append(self, key, kind, payload):
        _text(kind)
        encoded = _encode(payload)
        identity, _ = self._key(key)
        with self.transaction():
            sequence = len(self.events(key)) + 1
            self._db.execute('INSERT INTO events VALUES (?,?,?,?,?,?)',
                             (identity, sequence, kind, encoded, _digest(encoded), durable._time()))
        return sequence

    def operation(self, key, operation_key):
        _text(operation_key)
        current = None
        for event in self.events(key):
            payload = event['payload']
            if event['kind'] not in ('intent', 'result') or payload['operation_key'] != operation_key:
                continue
            if event['kind'] == 'intent':
                current = dict(intent=payload['content'], status='intent', result=None,
                               sequence=event['sequence'])
            else:
                current.update(status=payload['status'], result=payload['content'])
        return current

    def intent(self, key, operation_key, content):
        _text(operation_key)
        _encode(content)
        with self.transaction():
            previous = self.operation(key, operation_key)
            if previous is not None:
                if _encode(previous['intent']) != _encode(content):
                    raise StateConflict('Changed operation intent; retain original evidence')
                return previous['sequence']
            return self._append(key, 'intent', {'operation_key': operation_key, 'content': content})

    def result(self, key, operation_key, status, content):
        _encode(content)
        if status not in ('succeeded', 'write-unknown', 'safe-retry', 'ambiguous'):
            raise InvalidState('Invalid operation result status')
        with self.transaction():
            previous = self.operation(key, operation_key)
            if previous is None:
                raise StateConflict('Durable operation intent required before result')
            if previous['status'] == status and _encode(previous['result']) == _encode(content):
                return
            if previous['status'] in ('succeeded', 'ambiguous'):
                raise StateConflict('Terminal operation result retained; explicit recovery required')
            self._append(key, 'result', dict(operation_key=operation_key, status=status, content=content))

    def owner(self):
        self._available()
        row = self._db.execute('SELECT identity FROM owner').fetchone()
        return Owner(**json.loads(row[0])) if row is not None else None

    def claim_owner(self, owner):
        if not isinstance(owner, Owner):
            raise InvalidState('Invalid local owner identity')
        encoded = owner.encoded()
        if owner.checkout != self.root:
            raise StateConflict('Owner checkout must match canonical journal checkout')
        with self.transaction():
            previous = self.owner()
            if previous is not None:
                if previous != owner:
                    raise StateConflict('Competing owner; retain evidence for reconciliation')
                return
            self._db.execute('INSERT INTO owner VALUES (?)', (encoded,))

    def require_ownership(self, owner, port=None):
        """Check freshly on every call; no cached observation grants eligibility."""
        if self.owner() != owner or not isinstance(owner, Owner):
            raise StateConflict('Local ownership absent or conflicting')
        try:
            evidence = (port or UnavailableOwnership()).observe(self.repository, self.issue, owner)
            valid = (isinstance(evidence, OwnershipEvidence) and evidence.state == 'exclusive'
                     and evidence.repository == self.repository and evidence.issue == self.issue
                     and evidence.owner == owner and _text(evidence.verifier)
                     and _text(evidence.reference))
        except Exception:
            valid = False
        if not valid:
            raise StateConflict('Trusted cross-host ownership unavailable or conflicting')
        return evidence

    def _validate(self):
        with closing(sqlite3.connect(':memory:')) as expected:
            expected.executescript(SCHEMA)
            if _schema(self._db) != _schema(expected):
                raise InvalidState('Unsupported journal schema; retain evidence')
        if (self._db.execute('PRAGMA user_version').fetchone()[0] != 1
                or self._db.execute('PRAGMA integrity_check').fetchall()[0][0] != 'ok'
                or self._db.execute('PRAGMA foreign_key_check').fetchone() is not None):
            raise InvalidState('Corrupt journal retained; operator recovery required')
        if [tuple(row) for row in self._db.execute('SELECT * FROM issue')] != [
                (self.repository, self.issue)]:
            raise InvalidState('Journal issue identity conflict')
        if self._db.execute('SELECT count(*) FROM owner').fetchone()[0] > 1:
            raise InvalidState('Conflicting owner evidence retained')
        owner = self.owner()
        if owner is not None:
            owner.encoded()
            if owner.checkout != self.root:
                raise InvalidState('Journal checkout changed; retain ownership evidence')
        for record in self._db.execute('SELECT * FROM records'):
            key = HandoffKey(**json.loads(record['identity']))
            identity, encoded = self._key(key)
            if record['key'] != identity or record['identity'] != encoded:
                raise InvalidState('Corrupt handoff identity retained')
            operations = {}
            for sequence, event in enumerate(self.events(key), 1):
                _text(event['kind'])
                if event['sequence'] != sequence or _digest(_encode(event['payload'])) != event['digest']:
                    raise InvalidState('Corrupt event evidence retained')
                payload = event['payload']
                if event['kind'] == 'intent':
                    if (not isinstance(payload, dict) or set(payload) != {'operation_key', 'content'}
                            or _text(payload['operation_key']) in operations):
                        raise InvalidState('Conflicting operation intent retained')
                    operations[payload['operation_key']] = 'intent'
                elif event['kind'] == 'result':
                    if (not isinstance(payload, dict)
                            or set(payload) != {'operation_key', 'status', 'content'}
                            or _text(payload['operation_key']) not in operations
                            or operations[payload['operation_key']] in ('succeeded', 'ambiguous')
                            or payload['status'] not in ('succeeded', 'write-unknown', 'safe-retry',
                                                         'ambiguous')):
                        raise InvalidState('Conflicting operation result retained')
                    operations[payload['operation_key']] = payload['status']


@contextmanager
def open_journal(root, repository, issue):
    """Open one separate ignored per-issue journal under a whole-command lock.

    Never repairs/deletes unknown evidence or accesses Feature 003's journal.
    A missing database in an existing issue directory requires operator recovery.
    """
    _text(repository)
    _text(issue)
    name = _digest(_encode([repository, issue]))
    descriptors = []
    connection = None
    try:
        root = Path(root)
        if (not root.is_absolute() or root != root.resolve()
                or root.is_symlink() or not root.is_dir()):
            raise InvalidState('Journal root must be an existing unsymlinked directory')
        root = root.resolve()
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        descriptors.append(root_fd)
        durable._local_filesystem(root_fd)
        parent = durable._directory(root_fd, '.symphony', create=True)
        descriptors.append(parent)
        parent = durable._directory(parent, 'task-handoff', create=True)
        descriptors.append(parent)
        new = name not in os.listdir(parent)
        directory = durable._directory(parent, name, create=True)
        descriptors.append(directory)
        for child in os.listdir(directory):
            if (child not in ('lock', 'journal.sqlite3')
                    and not re.fullmatch(r'candidate-[0-9a-f]{32}\.sqlite3', child)):
                raise InvalidState('Unknown journal evidence retained; operator recovery required')
            durable._regular(directory, child, required=True)
        durable._regular(directory, 'lock')
        lock = os.open('lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC,
                       0o600, dir_fd=directory)
        descriptors.append(lock)
        durable._regular(directory, 'lock', required=True)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno in (errno.EACCES, errno.EAGAIN):
                raise StateConflict('Another command owns this issue journal lock') from error
            raise PersistenceFailure('Local process locking unavailable') from error
        os.fsync(lock)
        os.fsync(directory)
        existing = durable._regular(directory, 'journal.sqlite3')
        if not existing and not new:
            raise InvalidState('Journal missing; retain ownership evidence for recovery')
        connection = sqlite3.connect(':memory:', isolation_level=None)
        if not all(callable(getattr(connection, method, None))
                   for method in ('serialize', 'deserialize')):
            raise PersistenceFailure('Bounded SQLite snapshot APIs unavailable; Python 3.11+ required')
        connection.row_factory = sqlite3.Row
        loaded_identity = None
        if existing:
            fd = os.open('journal.sqlite3', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                         dir_fd=directory)
            try:
                info = os.fstat(fd)
                if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                        or info.st_size > MAX_DATABASE):
                    raise InvalidState('Unsafe or oversized journal retained')
                with os.fdopen(fd, 'rb', closefd=False) as stream:
                    snapshot = stream.read(MAX_DATABASE + 1)
                if len(snapshot) > MAX_DATABASE:
                    raise InvalidState('Journal growth limit exceeded; retain evidence')
                loaded_identity = info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns
                after = os.fstat(fd)
                if loaded_identity != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns):
                    raise InvalidState('Journal changed during read; retain evidence')
                connection.deserialize(snapshot)
            finally:
                os.close(fd)
        else:
            connection.executescript(SCHEMA)
            connection.execute('INSERT INTO issue VALUES (?,?)', (repository, issue))
        connection.execute('PRAGMA foreign_keys=ON')
        page_size = connection.execute('PRAGMA page_size').fetchone()[0]
        connection.execute('PRAGMA max_page_count=' + str(MAX_DATABASE // page_size))
        journal = Journal(connection, directory, root, repository, issue)
        journal._validate()
        if existing and (journal._file_identity != loaded_identity
                         or journal._identity('journal.sqlite3') != loaded_identity):
            raise InvalidState('Journal file changed during validation; retain evidence')
        if not existing:
            journal._persist()
        yield journal
    except sqlite3.DatabaseError as error:
        raise InvalidState('Journal unavailable or corrupt; retain evidence for recovery') from error
    except (ValueError, TypeError, KeyError, UnicodeError) as error:
        raise InvalidState('Invalid journal evidence retained; operator recovery required') from error
    except OSError as error:
        raise PersistenceFailure('Journal durability unavailable; retain evidence') from error
    finally:
        if connection is not None:
            connection.close()
        for descriptor in reversed(descriptors):
            os.close(descriptor)
