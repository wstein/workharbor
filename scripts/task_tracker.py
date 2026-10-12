"""Read-only host-injected approval/issue boundary; no credential acquisition.

The host owns transport authentication, provider parsing and immutable configuration.
The transport must return structured decisions authenticated from provider objects,
never approval claims extracted from issue prose. These fixtures/interfaces prove no
live provider capability. This module performs no network, files, logging or children;
credentials remain exclusively inside the injected host transport. No write API exists.
"""
from dataclasses import dataclass
import hashlib
import re
import time
from types import MappingProxyType
from typing import Protocol

from approval_binding import ApprovalTarget, ApprovalVerification
from task_package import parse_json


ACTIONS = frozenset(('spec', 'plan', 'execution', 'matrix', 'rework', 'review', 'publication'))
MAX_BYTES = 1024 * 1024


class TrackerBlocked(ValueError):
    """Fixed safe reason only; provider diagnostics never cross this boundary."""


class TransientRead(Exception):
    """Host marks only a retryable read failure; never used for writes."""


def _match(value, pattern):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def _id(value):
    return _match(value, r'[A-Za-z0-9_-]{1,128}')


def _repository(value):
    return _match(value, r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+') and len(value) <= 256


def _epoch(value):
    return type(value) is int and 0 <= value <= 253402300799


def _keys(value, expected):
    return type(value) is dict and set(value) == set(expected)


@dataclass(frozen=True)
class TrackerConfig:
    provider_id: str
    repository: str
    repository_id: str
    owner_id: str
    verifier_id: str
    max_observation_age: int = 300
    max_decision_age: int = 2592000


@dataclass(frozen=True)
class DecisionTarget:
    repository: str
    feature: str
    task_id: str
    action: str
    reviewed_revision: str
    reviewed_package_digest: str
    binding_digest: str


@dataclass(frozen=True)
class ApprovalReference:
    object_id: str
    url: str
    content_digest: str
    target: DecisionTarget


@dataclass(frozen=True)
class AuthenticatedRead:
    provider_id: str
    repository_id: str
    verifier_id: str
    observed_at: int
    payload: bytes


class ReadTransport(Protocol):
    def read(self, kind: str, object_id: str) -> AuthenticatedRead: ...


@dataclass(frozen=True)
class ApprovalDecision:
    provider_id: str | None
    repository_id: str | None
    object_id: str
    url: str
    actor_id: str | None
    owner_id: str | None
    target: DecisionTarget
    object_digest: str
    state: str | None
    created_at: int | None
    decided_at: int | None
    updated_at: int | None
    expires_at: int | None
    observed_at: int | None
    assurance: str
    verifier_id: str | None


@dataclass(frozen=True)
class IssueIdentity:
    repository: str
    number: int


@dataclass(frozen=True)
class IssueObservation:
    provider_id: str
    repository_id: str
    object_id: str
    repository: str
    number: int
    state: str
    labels: tuple[str, ...]
    updated_at: int
    object_digest: str
    observed_at: int
    verifier_id: str


def _target_valid(target):
    return (type(target) is DecisionTarget and _repository(target.repository)
            and _match(target.feature, r'[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*')
            and _match(target.task_id, r'T[0-9]{3}') and type(target.action) is str
            and target.action in ACTIONS
            and _match(target.reviewed_revision, r'[0-9a-f]{40}|[0-9a-f]{64}')
            and all(_match(value, r'[0-9a-f]{64}') for value in
                    (target.reviewed_package_digest, target.binding_digest)))


def _reference_valid(reference):
    if (type(reference) is not ApprovalReference or not _id(reference.object_id)
            or not _target_valid(reference.target)
            or not _match(reference.content_digest, r'[0-9a-f]{64}')):
        return False
    repository = re.escape(reference.target.repository)
    return _match(reference.url, r'https://github\.com/' + repository
                  + r'/(?:issues|pull)/[1-9][0-9]*(?:#(?:issuecomment|pullrequestreview|discussion_r)-[1-9][0-9]*)?')


def supplied_local(reference):
    """Record inert supplied evidence without manufacturing an actor or verifier."""
    if not _reference_valid(reference):
        raise TrackerBlocked('invalid-approval-reference')
    return ApprovalDecision(None, None, reference.object_id, reference.url, None, None,
                            reference.target, reference.content_digest, None, None, None,
                            None, None, None, 'supplied-local', None)


class AuthenticatedTracker:
    """A trusted host transport is mandatory; payloads never select a provider.

    Fresh refetch is mandatory on every call. Reference pins must come from trusted
    host configuration, not provider prose. Digests identify exact structured object
    bytes returned by the adapter, including all state/timestamps. Missing live
    observations leave capability claims empty, even for successful fixture reads.
    """
    capabilities = MappingProxyType({})

    def __init__(self, config, transport, *, clock=time.time):
        if (type(config) is not TrackerConfig or config.provider_id != 'github'
                or not _repository(config.repository)
                or not all(_id(value) for value in
                           (config.repository_id, config.owner_id, config.verifier_id))
                or any(type(value) is not int or not 1 <= value <= 31536000
                       for value in (config.max_observation_age, config.max_decision_age))
                or not callable(getattr(transport, 'read', None)) or not callable(clock)):
            raise TrackerBlocked('invalid-tracker-configuration')
        self.config = config
        self._transport = transport
        self._clock = clock

    def _read(self, kind, object_id):
        response = None
        # Initial read plus at most three retries; only explicitly transient reads.
        for attempt in range(4):
            transient = False
            try:
                response = self._transport.read(kind, object_id)
            except TransientRead:
                transient = True
            except Exception:
                pass
            else:
                break
            if not transient or attempt == 3:
                raise TrackerBlocked('tracker-read-unavailable')
        valid_clock = False
        try:
            now = self._clock()
            valid_clock = type(now) in (int, float) and 0 <= now <= 253402300799
        except Exception:
            pass
        if not valid_clock:
            raise TrackerBlocked('tracker-clock-unavailable')
        config = self.config
        if (type(response) is not AuthenticatedRead
                or (response.provider_id, response.repository_id, response.verifier_id)
                != (config.provider_id, config.repository_id, config.verifier_id)
                or not _epoch(response.observed_at)
                or not 0 <= now - response.observed_at <= config.max_observation_age
                or type(response.payload) is not bytes or len(response.payload) > MAX_BYTES):
            raise TrackerBlocked('tracker-authentication-unavailable')
        value = None
        try:
            value = parse_json(response.payload)
        except Exception:
            pass
        if type(value) is not dict:
            raise TrackerBlocked('invalid-provider-object')
        return value, response, now

    def read_approval(self, reference):
        if (not _reference_valid(reference)
                or reference.target.repository != self.config.repository):
            raise TrackerBlocked('invalid-approval-reference')
        value, response, now = self._read('approval', reference.object_id)
        keys = ('object_id', 'repository', 'actor_id', 'state', 'created_at',
                'decided_at', 'updated_at', 'expires_at', 'target')
        target = None
        if _keys(value, keys) and _keys(value['target'], DecisionTarget.__dataclass_fields__):
            target = DecisionTarget(**value['target'])
        if (target != reference.target or not _target_valid(target)
                or value.get('object_id') != reference.object_id
                or value.get('repository') != self.config.repository
                or value.get('actor_id') != self.config.owner_id
                or value.get('state') != 'approved'
                or not all(_epoch(value.get(key)) for key in
                           ('created_at', 'decided_at', 'updated_at', 'expires_at'))
                or not value['created_at'] <= value['decided_at'] == value['updated_at'] <= now
                or not 0 <= now - value['decided_at'] <= self.config.max_decision_age
                or not now < value['expires_at']
                or hashlib.sha256(response.payload).hexdigest() != reference.content_digest):
            raise TrackerBlocked('approval-not-current-or-bound')
        return ApprovalDecision(self.config.provider_id, self.config.repository_id,
                                reference.object_id, reference.url, value['actor_id'],
                                self.config.owner_id, target, reference.content_digest,
                                value['state'], value['created_at'], value['decided_at'],
                                value['updated_at'], value['expires_at'], response.observed_at,
                                'authenticated-provider', response.verifier_id)

    def read_issue(self, identity):
        if (type(identity) is not IssueIdentity or identity.repository != self.config.repository
                or type(identity.number) is not int or not 1 <= identity.number <= 2147483647):
            raise TrackerBlocked('invalid-issue-identity')
        value, response, now = self._read('issue', str(identity.number))
        if (not _keys(value, ('object_id', 'repository', 'number', 'state', 'labels', 'updated_at'))
                or not _id(value['object_id']) or value['repository'] != identity.repository
                or type(value['number']) is not int or value['number'] != identity.number
                or value['state'] not in ('open', 'closed')
                or not _epoch(value['updated_at']) or value['updated_at'] > now
                or type(value['labels']) is not list or len(value['labels']) > 100
                or any(not _match(label, r'[A-Za-z0-9][A-Za-z0-9 ._/-]{0,49}')
                       for label in value['labels']) or len(set(value['labels'])) != len(value['labels'])):
            raise TrackerBlocked('invalid-provider-issue')
        return IssueObservation(self.config.provider_id, self.config.repository_id,
                                value['object_id'], identity.repository, identity.number,
                                value['state'], tuple(value['labels']), value['updated_at'],
                                hashlib.sha256(response.payload).hexdigest(), response.observed_at,
                                response.verifier_id)


class ApprovalAuthority:
    """Existing approval_binding port, with host-pinned feature/task/reference map."""
    def __init__(self, tracker, feature, task_id, references):
        if (type(tracker) is not AuthenticatedTracker or type(references) is not dict
                or len(references) > 100
                or not _match(feature, r'[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*')
                or not _match(task_id, r'T[0-9]{3}')
                or any(not _reference_valid(ref) or url != ref.url
                       for url, ref in references.items())):
            raise TrackerBlocked('invalid-approval-authority')
        self._tracker = tracker
        self._feature = feature
        self._task_id = task_id
        self._references = MappingProxyType(dict(references))

    def verify(self, approval_ref, target):
        if type(approval_ref) is not str or type(target) is not ApprovalTarget:
            raise TrackerBlocked('invalid-approval-target')
        reference = self._references.get(approval_ref)
        expected = DecisionTarget(self._tracker.config.repository, self._feature, self._task_id,
                                  target.action, target.reviewed_revision,
                                  target.reviewed_package_digest, target.binding_digest)
        if reference is None or reference.target != expected:
            raise TrackerBlocked('approval-target-mismatch')
        decision = self._tracker.read_approval(reference)
        return ApprovalVerification(True, decision.assurance, target, decision.url)
