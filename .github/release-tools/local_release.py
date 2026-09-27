#!/usr/bin/env python3
"""Build and optionally publish the local four-part Linux amd64 release."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

from github_release import GitHub, access_token, preflight, publish
from release_assets import package, sha256, validate_binary, verify

ROOT = Path(__file__).resolve().parents[2]
VERSION_FILE = ROOT / 'backend/cmd/server/VERSION'
VERSION_RE = re.compile(r'0\.2\.8\.(0|[1-9][0-9]*)')
SOURCE_PATHS = ['backend/cmd', 'backend/internal', 'backend/ent', 'backend/migrations',
                'backend/resources', 'backend/go.mod', 'backend/go.sum', 'frontend/src',
                'frontend/public', 'frontend/package.json', 'frontend/pnpm-lock.yaml',
                'frontend/index.html', 'frontend/vite.config.ts', 'frontend/tsconfig*.json',
                'frontend/tailwind.config.*', 'frontend/postcss.config.*', 'deploy', '.github',
                ':(exclude)backend/internal/web/dist', ':(exclude)**/__pycache__/**']


def run(args, *, cwd=ROOT, env=None, capture=False):
    print('+ ' + ' '.join(str(arg) for arg in args), flush=True)
    return subprocess.run([str(arg) for arg in args], cwd=cwd, env=env, check=True,
                          stdout=subprocess.PIPE if capture else None,
                          text=True, encoding='utf-8', errors='replace').stdout


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT).decode('utf-8').strip()


def source_state():
    paths = git('ls-files', '-c', '-o', '--exclude-standard', '-z', '--', *SOURCE_PATHS).split('\0')
    digest = hashlib.sha256()
    for name in sorted(set(paths) - {''}):
        path = ROOT / name
        digest.update(name.encode() + b'\0')
        digest.update(bytes.fromhex(sha256(path)) if path.is_file() else b'DELETED')
    changes = git('status', '--porcelain', '--untracked-files=all', '--', *SOURCE_PATHS)
    return {'commit': git('rev-parse', 'HEAD'), 'source_sha256': digest.hexdigest(),
            'source_modified': bool(changes)}


def read_version():
    value = VERSION_FILE.read_text(encoding='utf-8').strip()
    if not VERSION_RE.fullmatch(value):
        raise ValueError('VERSION must use the local 0.2.8.x series; Ranxi versions are separate.')
    return value


def tool(name, override=None):
    candidate = override or shutil.which(name + '.cmd' if os.name == 'nt' and name == 'pnpm' else name)
    if not candidate or not Path(candidate).is_file():
        raise RuntimeError(f'{name} is unavailable; set PATH or provide --{name}.')
    return str(Path(candidate).resolve())


def build_environment():
    env = dict(os.environ, CGO_ENABLED='0', GOARCH='amd64', GOFLAGS='-mod=readonly')
    env.setdefault('GOMAXPROCS', '2')
    env.setdefault('GOGC', '50')
    return env


def run_checks(go, pnpm, env):
    run([sys.executable, '-m', 'unittest', 'discover', '-s', '.github/release-tools',
         '-p', 'test_local_release*.py'])
    run([pnpm, '--dir', 'frontend', 'exec', 'vitest', 'run',
         'src/components/common/__tests__/VersionBadge.spec.ts',
         'src/stores/__tests__/appVersion.spec.ts'])
    run([pnpm, '--dir', 'frontend', 'typecheck'])
    run([pnpm, '--dir', 'frontend', 'build'])
    host_env = {k: v for k, v in env.items() if k != 'GOOS'}
    run([go, 'test', '-tags', 'unit', '-p', '1', '-count=1', './internal/service',
         '-run', '^TestUpdateService'], cwd=ROOT / 'backend', env=host_env)
    run([tool('bash'), 'deploy/tests/install-github-token-test.sh'])
    regression = ROOT / 'deploy/tests/install-release-test.sh'
    if regression.exists():
        run([tool('bash'), str(regression)])


def create_build(args, state, output):
    go, pnpm = tool('go', args.go), tool('pnpm', args.pnpm)
    env = build_environment()
    version = read_version()
    output.mkdir(parents=True, exist_ok=True)
    cached = output / 'build-info.json'
    if cached.exists():
        info = json.loads(cached.read_text(encoding='utf-8'))
        if all(info.get(k) == v for k, v in state.items()) and info.get('version') == version:
            assets = verify(output, info)
            print('Reusing the verified package for the same source and version.', flush=True)
            return info, assets
        raise RuntimeError('Existing package belongs to different source. Use a new version/output directory.')
    run_checks(go, pnpm, env)
    stamp = datetime.fromtimestamp(int(git('show', '-s', '--format=%ct', state['commit'])), timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    binary = output / f"sub2api-linux-amd64-v{version}-{state['commit'][:9]}"
    flags = (f"-s -w -X main.Version={version} -X main.Commit={state['commit']} "
             f'-X main.Date={stamp} -X main.BuildType=release')
    run([go, 'build', '-p', '1', '-tags', 'embed', '-trimpath', '-ldflags', flags,
         '-o', binary, './cmd/server'], cwd=ROOT / 'backend', env=dict(env, GOOS='linux'))
    metadata = run([go, 'version', '-m', binary], capture=True)
    info = dict(state, version=version, ranxi_version=(ROOT / 'backend/cmd/server/RANXI_VERSION').read_text().strip(),
                build_date_utc=stamp, target='linux/amd64', build_type='release', cgo_enabled=0,
                tags=['embed'], linker_flags=flags, binary_size=binary.stat().st_size, binary_sha256=sha256(binary),
                vcs_modified='vcs.modified=true' in metadata)
    validate_binary(binary, metadata, info)
    if source_state() != state or read_version() != version:
        raise RuntimeError('Source changed during validation/build; package was not published.')
    (output / 'go-version.txt').write_text(metadata, encoding='utf-8', newline='\n')
    return info, package(ROOT, binary, info)


def prepare_version():
    current = read_version()
    committed = git('show', 'HEAD:backend/cmd/server/VERSION')
    if current != committed:
        print(f'VERSION already differs from HEAD: {current}. Reusing it without another increment.')
        return
    parts = current.split('.')
    parts[-1] = str(int(parts[-1]) + 1)
    version = '.'.join(parts)
    VERSION_FILE.write_text(version + '\n', encoding='utf-8', newline='\n')
    print(f'Prepared {version}. Review, commit and push source before publish. Retry publish without prepare.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['prepare', 'check', 'build', 'publish'])
    parser.add_argument('--go', help='Go executable path (use a 64-bit host toolchain)')
    parser.add_argument('--pnpm', help='pnpm executable path')
    parser.add_argument('--output', type=Path, help='Output directory, default release/vVERSION')
    parser.add_argument('--expected-tag', help='Require this tag to match VERSION (for CI)')
    args = parser.parse_args()
    if args.command == 'prepare':
        prepare_version()
        return
    version, state = read_version(), source_state()
    if args.expected_tag and args.expected_tag != 'v' + version:
        raise RuntimeError('Selected release tag does not match the committed VERSION file.')
    output = (args.output or ROOT / 'release' / ('v' + version)).resolve()
    if args.command == 'check':
        print(json.dumps(dict(state, version=version, output=str(output)), indent=2))
        preflight(GitHub(access_token()), state['commit'])
        print('GitHub authentication and source commit verified.')
        return
    client = None
    if args.command == 'publish':
        if state['source_modified']:
            raise RuntimeError('Uncommitted source detected. Review, commit and push it before publish; build is available for local testing.')
        client = GitHub(access_token())
        preflight(client, state['commit'])
    info, assets = create_build(args, state, output)
    print(json.dumps({'directory': str(output), 'version': version, 'source_modified': state['source_modified'],
                      'assets': [{'name': p.name, 'size': p.stat().st_size, 'sha256': sha256(p)} for p in assets]}, indent=2))
    if client:
        if source_state() != state:
            raise RuntimeError('Source changed after build; publication stopped.')
        print('Published: ' + publish(client, info, assets))
    else:
        print('Local package ready. No GitHub changes were made.')


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, OSError, subprocess.CalledProcessError) as error:
        print('Release failed: ' + str(error), file=sys.stderr)
        sys.exit(1)
