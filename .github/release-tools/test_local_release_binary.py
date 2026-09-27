"""Verify trimmed Go release metadata without relying on omitted linker flags."""
from contextlib import ExitStack, redirect_stdout
from copy import deepcopy
import io
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import local_release as release
import release_assets as assets


class BinaryValidationTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.binary = self.root / 'sub2api'
        self.info = {'version': '0.2.8.24', 'commit': 'a' * 40,
                     'build_date_utc': '2026-09-27T08:03:13Z', 'build_type': 'release'}
        self.flags = ('-s -w -X main.Version=0.2.8.24 -X main.Commit=' + 'a' * 40 +
                      ' -X main.Date=2026-09-27T08:03:13Z -X main.BuildType=release')
        self.info['linker_flags'] = self.flags
        header = bytearray(20)
        header[:6], header[18:20] = b'\x7fELF\x02\x01', (62).to_bytes(2, 'little')
        self.payload = bytes(header) + b''.join(self.info[key].encode() + b'\0'
                                              for key in ('version', 'commit', 'build_date_utc'))
        self.binary.write_bytes(self.payload)
        self.metadata = '\n'.join('\tbuild\t' + entry for entry in (
            '-tags=embed', '-trimpath=true', 'GOOS=linux', 'GOARCH=amd64', 'CGO_ENABLED=0',
            'vcs.revision=' + self.info['commit'], 'vcs.modified=true'))

    def test_trimpath_metadata_without_ldflags_is_valid(self):
        self.assertNotIn('-ldflags', self.metadata)
        assets.validate_binary(self.binary, self.metadata, self.info)

    def test_wrong_architecture_and_exact_metadata_mismatch_are_rejected(self):
        self.binary.write_bytes(b'Not ELF' + self.payload)
        with self.assertRaisesRegex(ValueError, 'Linux amd64 ELF'):
            assets.validate_binary(self.binary, self.metadata, self.info)
        self.binary.write_bytes(self.payload)
        for original, replacement in (('GOOS=linux', 'GOOS=linux-extra'),
                                      ('GOARCH=amd64', 'GOARCH=arm64'),
                                      ('CGO_ENABLED=0', 'CGO_ENABLED=1'),
                                      ('-tags=embed', '-tags=other'),
                                      ('-trimpath=true', '-trimpath=false'),
                                      ('vcs.revision=' + 'a' * 40, 'vcs.revision=' + 'b' * 40)):
            with self.subTest(field=original), self.assertRaisesRegex(ValueError, 'build metadata'):
                assets.validate_binary(self.binary, self.metadata.replace(original, replacement), self.info)

    def test_missing_embedded_identity_is_rejected(self):
        for key in ('version', 'commit', 'build_date_utc'):
            self.binary.write_bytes(self.payload.replace(self.info[key].encode() + b'\0', b'wrong\0'))
            with self.subTest(field=key), self.assertRaisesRegex(ValueError, 'embedded binary identity: ' + key):
                assets.validate_binary(self.binary, self.metadata, self.info)

    def test_wrong_linker_identity_and_source_build_type_are_rejected(self):
        for key, value in (('build_type', 'source'), ('linker_flags', self.flags.replace('release', 'source'))):
            info = dict(self.info, **{key: value})
            with self.subTest(field=key), self.assertRaisesRegex(ValueError, 'Release linker arguments'):
                assets.validate_binary(self.binary, self.metadata, info)

    def test_tag_version_mismatch_stops_before_auth_build_or_publication(self):
        state = {'commit': self.info['commit'], 'source_sha256': 'c' * 64, 'source_modified': False}
        with ExitStack() as stack:
            stack.enter_context(patch('sys.argv', ['local_release.py', 'publish', '--expected-tag', 'v0.2.8.999']))
            stack.enter_context(patch.object(release, 'read_version', return_value=self.info['version']))
            stack.enter_context(patch.object(release, 'source_state', return_value=state))
            auth = stack.enter_context(patch.object(release, 'access_token'))
            build = stack.enter_context(patch.object(release, 'create_build'))
            publish = stack.enter_context(patch.object(release, 'publish'))
            with self.assertRaisesRegex(RuntimeError, 'tag does not match the committed VERSION'):
                release.main()
            auth.assert_not_called()
            build.assert_not_called()
            publish.assert_not_called()

    def test_build_passes_and_records_release_flags_with_trimpath(self):
        ranxi = self.root / 'backend/cmd/server/RANXI_VERSION'
        ranxi.parent.mkdir(parents=True)
        ranxi.write_text('2.8.18', encoding='utf-8')
        state = {'commit': self.info['commit'], 'source_sha256': 'c' * 64, 'source_modified': True}
        calls, saved = [], {}

        def run(args, **kwargs):
            calls.append(list(map(str, args)))
            if args[1] == 'build':
                Path(args[args.index('-o') + 1]).write_bytes(self.payload)
            return self.metadata if args[1] == 'version' else None

        def package(root, binary, info):
            saved.update(deepcopy(info))
            return []

        with ExitStack() as stack:
            for name, value in (('ROOT', self.root), ('read_version', lambda: self.info['version']),
                                ('tool', lambda name, override=None: name), ('run_checks', lambda *args: None),
                                ('git', lambda *args: '1790496193'), ('run', run),
                                ('source_state', lambda: state), ('package', package)):
                stack.enter_context(patch.object(release, name, value))
            stack.enter_context(redirect_stdout(io.StringIO()))
            release.create_build(SimpleNamespace(go=None, pnpm=None), state, self.root / 'output')
        build = next(call for call in calls if call[1] == 'build')
        self.assertIn('-trimpath', build)
        self.assertEqual(build[build.index('-ldflags') + 1], self.flags)
        self.assertEqual(saved['linker_flags'], self.flags)
        self.assertEqual(saved['build_type'], 'release')


if __name__ == '__main__':
    unittest.main()
