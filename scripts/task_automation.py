#!/usr/bin/env python3
"""Task preparation, preview and host-injected fail-closed readiness."""
import argparse
from pathlib import Path
import re
import sys

# Resolve adjacent trusted tooling explicitly, including under python -I -B.
sys.path.insert(0, str(Path(__file__).resolve().parent))
import task_outcomes as outcomes
import task_package as package
import task_handoff as handoff

TRANSPORT_RETRY_LIMIT = 3  # No transport is called by these offline commands.


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise package.PackageError('invalid-arguments')


def envelope():
    return dict(schema_version=1, package_digest=None, revision_digest=None,
                requested_mode=None, effective_mode=None, progress='blocked',
                offline_result='blocked', structural_valid=False, execution_eligible=False,
                approval_assurance=None, authenticated_approval=None, independent_review=None,
                live_execution=None, raw_exits=[], blockers=[], next_action='review-blockers',
                transport_retry_limit=TRANSPORT_RETRY_LIMIT, transport_attempts=0)


def relative_path(root, name):
    if name.startswith('/'):
        prefix = str(root) + '/'
        if not name.startswith(prefix):
            raise package.PackageError('unsafe-repository-path')
        name = name[len(prefix):]
    package._path_parts(name)
    return name


def repository_identity(root):
    # Local origin metadata identifies the host repository; package text cannot
    # supply its own trusted identity. No fetch/provider request is performed.
    origin = package._git(root, 'config', '--get', 'remote.origin.url').decode().strip()
    matched = re.fullmatch(r'(?:https://github\.com/|git@github\.com:)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?', origin)
    if matched is None:
        raise package.PackageError('repository-identity-unavailable')
    return matched.group(1)


def summary(value, report):
    # Summaries/diagnostics omit opaque context, raw argv and output. The package
    # itself preserves exact reviewed input bytes/identity, without redaction.
    return dict(schema_version=1, repository=value['repository'], feature=value['feature'],
                task_id=value['task_id'], package_digest=value['package_digest'],
                revision_digest=package.package_revision_digest(value), mode=value['mode'],
                check_count=len(value['outcomes']),
                case_count=sum(len(check['cases']) for check in value['outcomes']),
                execution_eligible=False, authenticated_approval=None,
                independent_review=None, live_execution=None, raw_exits=[],
                blockers=report['blockers'], next_action='review-blockers')


def validation_exit(report):
    codes = {blocker['code'] for blocker in report['blockers']}
    return 4 if 'invalid-outcomes' in codes else 3


def error_exit(code):
    if code == 'readiness-host-unavailable':
        return 3
    if code == 'output-conflict':
        return 5
    if code in ('output-unavailable', 'output-incomplete'):
        return 6
    if code.startswith(('unsafe-', 'repository-', 'git-')):
        return 3
    return 2


def ready(approved, current, snapshot, evidence, **ports):
    """One conditional eligibility write after two fresh complete observations.

    This is a host API. No package/configuration can construct its adapters.
    Unsupported provider writes are never attempted; an uncertain write is not
    retried and remains incomplete for operator reconciliation.
    """
    import task_tracker
    result = package.assess_readiness(approved, current, snapshot, evidence, **ports)
    if not result['readiness_valid']:
        return result
    result = package.revalidate_readiness(result, approved, current, snapshot, evidence,
                                         boundary='eligibility', **ports)
    if not result['readiness_valid']:
        return result
    try:
        if (ports.get('live') is not True or 'replace-labels' not in ports.get('authorized_actions', ())
                or 'replace-labels' not in current['execution']['external_actions']):
            raise package.PackageError('eligibility-action-not-authorized')
        tracker = ports['tracker']
        identity = task_tracker.IssueIdentity(current['repository'], current['issue']['number'])
        # Use the exact version/set from the final complete assessment. Another
        # provider read here could change source/ownership after its last proof.
        expected = dict(object_digest=result['issue_digest'], labels=tuple(result['issue_labels']))
        if 'workharbor-ready' in expected['labels']:
            result['execution_eligible'] = True
            return result
        desired = tuple(sorted((*expected['labels'], 'workharbor-ready')))
        # The provider must enforce this exact observed version/set atomically,
        # or prove serialization of all writers. A read/PUT/read is insufficient.
        written = tracker.replace_labels(identity, expected, desired)
        if written is not True:
            result['blockers'].append(dict(code='eligibility-write-incomplete'))
            return result
        result['execution_eligible'] = True
    except package.PackageError as error:
        result['readiness_valid'] = False
        result['blockers'].append(dict(code=error.code))
    except Exception:
        result['blockers'].append(dict(code='eligibility-write-uncertain'))
    return result


def main(argv=None, *, root=None, repository=None, readiness_host=None,
         recovery=None, recovery_ports=None, handoff_ports=None):
    """Host may inject repository identity; CLI uses local Git origin metadata."""
    result = envelope()
    try:
        parser = Parser(description=__doc__)
        commands = parser.add_subparsers(dest='command', required=True, parser_class=Parser)
        prepare = commands.add_parser('prepare')
        prepare.add_argument('--input', required=True)
        prepare.add_argument('--output', required=True)
        for name in ('validate', 'effective'):
            child = commands.add_parser(name)
            child.add_argument('--package', required=True)
            child.add_argument('--offline', action='store_true', required=True)
        for name in ('resume', 'handoff'):
            child = commands.add_parser(name)
            child.add_argument('--package', required=True)
            child.add_argument('--attempt', required=True)
            mode = child.add_mutually_exclusive_group(required=True)
            mode.add_argument('--offline', action='store_true')
            mode.add_argument('--live-config')
        child = commands.add_parser('ready')
        child.add_argument('--package', required=True)
        child.add_argument('--live-config', required=True)
        args = parser.parse_args(argv)
        root = Path.cwd() if root is None else Path(root)
        name = args.input if args.command == 'prepare' else args.package
        data = package.read_repository_file(root, relative_path(root, name))
        value = package.build_package(package.parse_json(data))
        if args.command != 'prepare' and 'package_digest' not in package.parse_json(data):
            raise package.PackageError('package-digest-missing')
        repository = repository if repository is not None else repository_identity(root)
        if args.command in ('resume', 'handoff'):
            result.update(package_digest=value['package_digest'],
                          revision_digest=package.package_revision_digest(value),
                          requested_mode=value['mode'], effective_mode=value['mode'],
                          resume_valid=False, handoff_complete=False)
            if recovery is None:
                result['blockers'] = [dict(code='recovery-host-unavailable')]
                code = 3
            elif (not isinstance(recovery, handoff.Recovery)
                    or recovery.key.guard_attempt_id != args.attempt
                    or recovery.key.repository != repository or recovery.journal.root != str(root.resolve())):
                result['blockers'] = [dict(code='recovery-ownership-conflict')]
                code = 5
            else:
                ports = dict((recovery_ports if args.command == 'resume' else handoff_ports) or {})
                ports['live'] = bool(args.live_config)
                execute = recovery.resume if args.command == 'resume' else (
                    lambda value, snapshot, evidence, **ports: handoff.handoff(
                        recovery, value, snapshot, evidence, **ports))
                observed = execute(value, ports.pop('snapshot', None), ports.pop('evidence', None), **ports)
                result.update(observed)
                codes = {item['code'] for item in result['blockers']}
                successful = result['resume_valid'] if args.command == 'resume' else result['handoff_complete']
                code = (0 if successful else 5 if 'recovery-ownership-conflict' in codes else
                        6 if codes & {'final-label-reconciliation-incomplete',
                                      'publication-remote-reconciliation-blocked'} else 3)
            print(package.canonical_json(result).decode('utf-8'))
            return code
        if args.command == 'ready':
            if value['repository'] != repository:
                raise package.PackageError('repository-mismatch')
            # Configuration is inert non-secret input. Only trusted embedding
            # code can provide ports; CLI defaults cannot acquire credentials,
            # instantiate providers or turn configuration claims into authority.
            config = package.parse_json(package.read_repository_file(
                root, relative_path(root, args.live_config)))
            if type(config) is not dict or not callable(readiness_host):
                raise package.PackageError('readiness-host-unavailable')
            host = readiness_host(value, config)
            result.update(ready(host['approved'], value, host['snapshot'], host['evidence'],
                                **host['ports']))
            result.update(requested_mode=value['mode'], effective_mode=value['mode'])
            code = 0 if result['execution_eligible'] else (
                6 if any(b['code'] in ('eligibility-write-incomplete', 'eligibility-write-uncertain')
                         for b in result['blockers']) else 3)
            result.update(progress='eligible' if code == 0 else 'blocked',
                          offline_result='not-requested')
            print(package.canonical_json(result).decode('utf-8'))
            return code
        snapshot = dict(root=root, repository=repository, approved=None, current=None)
        report = package.validate(value, snapshot, None, {}, outcome_validator=outcomes.validate_checks)
        result.update(report)
        result.update(requested_mode=value['mode'], effective_mode=value['mode'])
        if not report['structural_valid']:
            code = validation_exit(report)
        elif args.command == 'prepare':
            output = relative_path(root, args.output)
            artifacts = package.write_preparation(root, output, value, summary(value, report))
            result.update(progress='prepared', offline_result='structural-valid',
                          artifacts={name: artifacts[name] for name in ('package', 'summary')},
                          reused=artifacts['reused'])
            code = 0
        elif args.command == 'effective':
            result.update(progress='previewed', offline_result='preview', effective=dict(
                source=value['source'], execution=value['execution'],
                check_count=len(value['outcomes']),
                case_count=sum(len(check['cases']) for check in value['outcomes']),
                checks_digest=outcomes.checks_digest(value['outcomes'])))
            code = 0
        else:
            code = 3 if report['blockers'] else 0
            result.update(progress='blocked' if code else 'validated-offline',
                          offline_result='blocked' if code else 'validated')
    except package.PackageError as error:
        result['blockers'] = [dict(code=error.code)]
        code = error_exit(error.code)
    except handoff.PersistenceFailure:
        result['blockers'] = [dict(code='recovery-persistence-unavailable')]
        code = 6
    except handoff.InvalidState:
        result['blockers'] = [dict(code='recovery-evidence-invalid')]
        code = 6
    except Exception:
        result['blockers'] = [dict(code='offline-command-unavailable')]
        code = 2
    try:
        response = package.canonical_json(result)
    except package.PackageError:
        result = envelope()
        result['blockers'] = [dict(code='response-limit-exceeded')]
        response = package.canonical_json(result)
        code = 2
    print(response.decode('utf-8'))
    return code


if __name__ == '__main__':
    raise SystemExit(main())
