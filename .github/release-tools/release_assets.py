"""Create and verify the Linux amd64 assets consumed by install.sh and the updater."""
import hashlib
import gzip
import json
import mmap
from datetime import datetime
from pathlib import Path
import tarfile


def sha256(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def validate_binary(path, build_info, expected):
    with path.open('rb') as stream:
        header = stream.read(20)
    if header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != 62:
        raise ValueError('Expected a Linux amd64 ELF binary')
    settings = {}
    for line in build_info.splitlines():
        parts = line.strip().split(None, 1)
        if len(parts) == 2 and parts[0] == 'build' and '=' in parts[1]:
            key, value = parts[1].split('=', 1)
            settings[key] = value
    required = {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0',
                '-tags': 'embed', '-trimpath': 'true', 'vcs.revision': expected['commit']}
    for key, value in required.items():
        if settings.get(key) != value:
            raise ValueError('Missing or incorrect binary build metadata: ' + key + '=' + value)
    # Go 在 -trimpath 下不记录 -ldflags；保留实际构建参数，并核验注入的唯一标识。
    flags = (f"-s -w -X main.Version={expected['version']} -X main.Commit={expected['commit']} "
             f"-X main.Date={expected['build_date_utc']} -X main.BuildType=release")
    if expected.get('build_type') != 'release' or expected.get('linker_flags') != flags:
        raise ValueError('Release linker arguments do not match the selected build identity')
    with path.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as binary:
        for key in ('version', 'commit', 'build_date_utc'):
            if binary.find(expected[key].encode('ascii') + b'\0') < 0:
                raise ValueError('Missing embedded binary identity: ' + key)


def package(root, binary, info):
    output = binary.parent
    archive = output / f"sub2api_{info['version']}_linux_amd64.tar.gz"
    timestamp = int(datetime.fromisoformat(info['build_date_utc'].replace('Z', '+00:00')).timestamp())
    with archive.open('wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=timestamp) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as bundle:
            entry = bundle.gettarinfo(str(binary), arcname='sub2api')
            entry.mode, entry.uid, entry.gid, entry.uname, entry.gname = 0o755, 0, 0, '', ''
            entry.mtime = timestamp
            with binary.open('rb') as stream:
                bundle.addfile(entry, stream)
    installer = output / 'install.sh'
    installer.write_bytes((root / 'deploy/install.sh').read_bytes().replace(b'\r\n', b'\n'))
    metadata = output / 'build-info.json'
    metadata.write_text(json.dumps(info, ensure_ascii=False, indent=2) + '\n', encoding='utf-8', newline='\n')
    checksums = output / 'checksums.txt'
    checked = [archive, installer, metadata]
    checksums.write_text(''.join(f'{sha256(path)}  {path.name}\n' for path in checked), encoding='utf-8', newline='\n')
    verify(output, info)
    return checked + [checksums]


def verify(output, info):
    names = [f"sub2api_{info['version']}_linux_amd64.tar.gz", 'install.sh', 'build-info.json']
    lines = (output / 'checksums.txt').read_text(encoding='utf-8').splitlines()
    expected = {}
    for line in lines:
        parts = line.split()
        if len(parts) != 2 or parts[1] in expected:
            raise ValueError('Malformed or duplicate checksum entry')
        expected[parts[1]] = parts[0]
    if set(expected) != set(names):
        raise ValueError('Missing or unexpected checksummed assets')
    for name in names:
        if sha256(output / name) != expected[name]:
            raise ValueError('Release checksum mismatch: ' + name)
    saved = json.loads((output / 'build-info.json').read_text(encoding='utf-8'))
    if saved != info:
        raise ValueError('Build metadata differs from the selected source')
    with tarfile.open(output / names[0], 'r:gz') as bundle:
        members = bundle.getmembers()
        if len(members) != 1 or members[0].name != 'sub2api' or not members[0].isfile() or members[0].mode != 0o755:
            raise ValueError('Archive must contain exactly one executable sub2api file')
        with bundle.extractfile(members[0]) as stream:
            if hashlib.file_digest(stream, 'sha256').hexdigest() != info['binary_sha256']:
                raise ValueError('Archived binary differs from the verified build')
    return [output / name for name in names + ['checksums.txt']]
