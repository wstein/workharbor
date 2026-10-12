"""Compose accepted source helpers with unavailable-by-default guard scope ports.

Local object/patch evidence grants no approval, review or execution authority.
Feature 003 owns task context, intervening history and prerequisite decisions.
"""
import copy
import re
from pathlib import Path

import approval_binding as binding
import check_ci as ci
import prepare_publication as publication
import task_package as package


OID = re.compile(r'[0-9a-f]{40}\Z')


def _offline_repository(root):
    """Reject Git metadata that could lazily retrieve supposedly local objects."""
    configuration = ci.git(root, 'config', '--null', '--list', '--no-includes')
    for record in configuration.split(b'\0'):
        key = record.split(b'\n', 1)[0].lower()
        if (key == b'extensions.partialclone' or key.startswith(b'remote.')
                and key.endswith((b'.promisor', b'.partialclonefilter'))):
            raise ValueError('Partial source unavailable offline')
    objects = Path(ci.git(root, 'rev-parse', '--path-format=absolute',
                          '--git-path', 'objects').decode().strip())
    if (any((objects/'pack').glob('*.promisor'))
            or (objects/'info/alternates').exists() or (objects/'info/alternates').is_symlink()):
        raise ValueError('External source objects unavailable offline')


def _commit(root, revision):
    if (type(revision) is not str or not OID.fullmatch(revision)
            or ci.git(root, 'cat-file', '-t', revision) != b'commit\n'):
        raise ValueError('Exact commit required')
    return ci.git(root, 'rev-parse', revision + '^{tree}').decode().strip()


def _scope(snapshot, context, evidence, guard, mode, live):
    assessment = package.assess_capabilities('entry', mode, guard=guard, live=live)
    if not assessment['capabilities_satisfied']:
        return ['guard-source-capability-unavailable']
    try:
        if (type(guard.revision) is not str or not OID.fullmatch(guard.revision)
                or any(item['implementation_revision'] != guard.revision
                       for item in assessment['evidence'])):
            return ['guard-source-revision-mismatch']
        contract = binding._contract(Path(snapshot['root']), guard.validate_context(context, evidence))
        if ('rebase' in snapshot and tuple(snapshot['rebase']['protected_paths'])
                != contract.protected_paths):
            return ['guard-source-protected-inventory-mismatch']
    except Exception:
        return ['guard-source-context-blocked']
    try:
        # Never pass the integration wrapper as the task's authored head. The
        # guard receives every distinct identity and owns history/prerequisites.
        verdict = package._guard_verdict(guard.validate_scope(copy.deepcopy(snapshot), context, evidence))
        return [] if verdict.accepted else ['guard-source-scope-blocked']
    except Exception:
        return ['guard-source-scope-blocked']


def validate_source(root, accepted, checker, source_context, context, evidence, *,
                    guard=None, mode='implement', live=False, rebase_evidence=None):
    """Validate source identity and delegate real head/index/history scope.

    Inputs are explicit host pins and opaque guard-owned context/evidence. The
    result remains ineligible even when source_valid: approval, review, runtime
    and action authority are separate gates. No checks or remote calls run here.
    """
    result = dict(source_valid=False, execution_eligible=False, snapshot=None,
                  authenticated_approval=None, independent_review=None,
                  live_execution=None, blockers=[])
    try:
        root = Path(root)
        _offline_repository(root)
        observed = ci.source_context(root, *source_context)
        root = root.resolve(strict=True)
        accepted_tree = _commit(root, accepted)
        ci.source_identity(root, checker)
        snapshot = dict(root=str(root), accepted=accepted, accepted_tree=accepted_tree,
                        checker=checker, event=observed.event, base=observed.base,
                        head=observed.head, checkout=observed.checkout,
                        base_tree=_commit(root, observed.base),
                        head_tree=_commit(root, observed.head),
                        checkout_tree=_commit(root, observed.checkout))
        if rebase_evidence is not None:
            snapshot['rebase'] = copy.deepcopy(rebase_evidence)
        result['snapshot'] = snapshot
        result['blockers'] = _scope(snapshot, context, evidence, guard, mode, live)
        # A host adapter cannot turn worktree drift into a new accepted baseline.
        if ci.source_context(root, *observed) != observed:
            raise ValueError('Source changed')
        ci.source_identity(root, checker)
        result['source_valid'] = not result['blockers']
    except Exception:
        # Git/adapter output can contain paths or secrets; expose fixed codes.
        result['blockers'].append('invalid-or-unavailable-source')
    return result






def prepare_candidate(validated_source, request):
    """Explicitly request the existing bounded preparer; never publish.

    This is not called during offline validation. The caller separately owns
    network/check-execution authority and exact-candidate review/publication.
    Existing preparation owns ref observation, actual merge and checker execution.
    """
    blocked = dict(status='BLOCKED', blocker='source-or-publication-pins-blocked')
    try:
        if validated_source['source_valid'] is not True or validated_source['blockers']:
            return blocked
        snapshot = validated_source['snapshot']
        if any(request[key] != snapshot[field] for key, field in
               (('source', 'checker'), ('base', 'base'), ('head', 'head'))):
            return blocked
        # Recheck the pinned local candidate immediately before preparation.
        current = ci.SourceContext(snapshot['event'], snapshot['base'], snapshot['head'], snapshot['checkout'])
        _offline_repository(Path(snapshot['root']))
        ci.source_context(Path(snapshot['root']), *current)
        ci.source_identity(Path(snapshot['root']), snapshot['checker'])
        return publication.prepare(**request)
    except Exception:
        return blocked
