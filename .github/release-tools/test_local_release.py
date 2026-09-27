"""Offline regression tests for local release integrity and publication."""
from contextlib import ExitStack, redirect_stderr, redirect_stdout
from copy import deepcopy
import io
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlparse

import github_release as github
import local_release as release
import release_assets as assets


class FakeGitHub:
    """A small stateful server; requests exercise the real publication code."""
    def __init__(self, commit):
        self.commit, self.calls, self.release, self.tag = commit, [], None, None
        self.payloads, self.fail_upload, self.remote_commit = {}, None, commit
        self.omit_from_final, self.corrupt_upload, self.latest = None, None, None

    def request(self, method, path, *, data=None, content_type=None, raw=False):
        self.calls.append((method, path, deepcopy(data)))
        parsed = urlparse(path)
        endpoint = parsed.path.removeprefix('/repos/ccx1/sub2api')
        if parsed.hostname == 'uploads.github.com':
            name = parse_qs(parsed.query)['name'][0]
            if name == self.fail_upload:
                raise RuntimeError('simulated upload failure')
            asset = {'id': len(self.payloads) + 1, 'name': name, 'size': len(data)}
            self.payloads[asset['id']] = b'corrupt' if name == self.corrupt_upload else data
            self.release['assets'].append(asset)
            return deepcopy(asset)
        if method == 'GET':
            if endpoint == '':
                return {'permissions': {'push': True}}
            if endpoint.startswith('/commits/'):
                return {'sha': self.remote_commit} if self.remote_commit else None
            if endpoint.startswith('/git/ref/tags/'):
                return {'object': {'type': 'commit', 'sha': self.tag}} if self.tag else None
            if endpoint == '/releases':
                return [deepcopy(self.release)] if self.release else []
            if endpoint == '/releases/latest':
                return deepcopy(self.latest or (self.release if self.release and not self.release['draft'] else None))
            if endpoint.startswith('/releases/assets/'):
                assert raw, 'Binary asset verification must request raw bytes'
                return self.payloads[int(endpoint.rsplit('/', 1)[1])]
            if endpoint == '/releases/1':
                result = deepcopy(self.release)
                result['assets'] = [a for a in result['assets'] if a['name'] != self.omit_from_final]
                return result
        if method == 'POST' and endpoint == '/releases':
            self.release = dict(deepcopy(data), id=1, assets=[],
                                upload_url='https://uploads.github.com/releases/1/assets{?name,label}',
                                html_url='https://github.com/ccx1/sub2api/releases/tag/' + data['tag_name'])
            return deepcopy(self.release)
        if method == 'PATCH' and endpoint == '/releases/1':
            self.release.update(deepcopy(data))
            return deepcopy(self.release)
        if method == 'DELETE' and endpoint.startswith('/releases/assets/'):
            asset_id = int(endpoint.rsplit('/', 1)[1])
            self.release['assets'] = [a for a in self.release['assets'] if a['id'] != asset_id]
            self.payloads.pop(asset_id, None)
            return None
        raise AssertionError(f'Unexpected request: {method} {path}')

    def mutations(self):
        return [call for call in self.calls if call[0] != 'GET']


class ReleaseFixture(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        (self.root / 'deploy').mkdir()
        (self.root / 'deploy/install.sh').write_text('#!/bin/sh\nexit 0\n', encoding='utf-8')
        self.binary = self.root / 'sub2api-linux-amd64-v0.2.8.18-aaaaaaaaa'
        self.binary.write_bytes(b'local release binary fixture')
        self.info = {'version': '0.2.8.18', 'ranxi_version': '2.8.14', 'commit': 'a' * 40,
                     'binary_sha256': assets.sha256(self.binary), 'build_date_utc': '2026-09-27T08:03:29Z'}
        self.assets = assets.package(self.root, self.binary, self.info)
        self.client = FakeGitHub(self.info['commit'])

    def publish(self):
        return github.publish(self.client, self.info, self.assets)

    def assert_unpublished(self):
        self.assertFalse(any(call[0] == 'PATCH' for call in self.client.calls))
        if self.client.release:
            self.assertTrue(self.client.release['draft'])


class PublicationTests(ReleaseFixture):
    def test_draft_upload_verify_every_hash_then_publish_last(self):
        self.assertIn('/tag/v0.2.8.18', self.publish())
        mutations = self.client.mutations()
        self.assertTrue(mutations[0][2]['draft'])
        self.assertEqual(len(mutations), len(self.assets) + 2)
        self.assertEqual(self.client.calls[-1][0], 'PATCH')
        self.assertEqual(self.client.calls[-1][2]['make_latest'], 'true')
        downloads = [path for method, path, _ in self.client.calls
                     if method == 'GET' and '/releases/assets/' in path]
        self.assertEqual(len(downloads), 2 * len(self.assets))
        self.assertEqual({a['name'] for a in self.client.release['assets']}, {p.name for p in self.assets})

    def test_failed_upload_keeps_draft_and_retry_only_uploads_missing_files(self):
        self.client.fail_upload = self.assets[1].name
        with self.assertRaisesRegex(RuntimeError, 'upload failure'):
            self.publish()
        self.assert_unpublished()
        self.assertEqual(len(self.client.release['assets']), 1)
        first_asset = deepcopy(self.client.release['assets'][0])
        self.client.calls.clear()
        self.client.fail_upload = None
        self.publish()
        uploads = [call for call in self.client.calls if call[0] == 'POST']
        self.assertEqual(len(uploads), len(self.assets) - 1)
        self.assertEqual(self.client.release['assets'][0], first_asset)

    def test_published_release_is_idempotent_but_cannot_be_overwritten(self):
        expected_url = self.publish()
        self.client.tag = self.info['commit']
        self.client.calls.clear()
        self.assertEqual(self.publish(), expected_url)
        self.assertEqual(self.client.mutations(), [])
        self.assets[0].write_bytes(b'X' * self.assets[0].stat().st_size)
        with self.assertRaisesRegex(RuntimeError, 'Existing asset differs'):
            self.publish()
        self.assertEqual(self.client.mutations(), [])

    def test_tag_and_draft_source_mismatches_block_before_mutating(self):
        self.client.tag = 'b' * 40
        with self.assertRaisesRegex(RuntimeError, 'different source commit'):
            self.publish()
        self.assertEqual(self.client.mutations(), [])
        self.client.tag = None
        self.client.fail_upload = self.assets[0].name
        with self.assertRaises(RuntimeError):
            self.publish()
        self.client.release['target_commitish'] = 'b' * 40
        self.client.calls.clear()
        with self.assertRaisesRegex(RuntimeError, 'another source commit'):
            self.publish()
        self.assertEqual(self.client.mutations(), [])

    def test_corrupt_upload_or_incomplete_final_inventory_never_publishes(self):
        for field in ('corrupt_upload', 'omit_from_final'):
            with self.subTest(field=field):
                self.client = FakeGitHub(self.info['commit'])
                setattr(self.client, field, self.assets[0].name)
                with self.assertRaisesRegex(RuntimeError, 'verification failed|not complete'):
                    self.publish()
                self.assert_unpublished()

    def test_missing_remote_source_commit_blocks_all_mutations(self):
        self.client.remote_commit = None
        with self.assertRaisesRegex(RuntimeError, 'Push the reviewed source commit'):
            self.publish()
        self.assertEqual(self.client.mutations(), [])

    def test_failed_empty_starter_asset_is_replaced_on_draft_retry(self):
        self.client.fail_upload = self.assets[0].name
        with self.assertRaises(RuntimeError):
            self.publish()
        self.client.release['assets'].append({'id': 99, 'name': self.assets[0].name, 'size': 0, 'state': 'starter'})
        self.client.fail_upload = None
        self.client.calls.clear()
        self.publish()
        mutations = self.client.mutations()
        self.assertEqual(mutations[0][:2], ('DELETE', '/repos/ccx1/sub2api/releases/assets/99'))
        self.assertEqual([c[0] for c in mutations], ['DELETE'] + ['POST'] * len(self.assets) + ['PATCH'])

    def test_newer_remote_release_blocks_moving_latest_backwards(self):
        self.client.latest = {'tag_name': 'v0.2.8.100'}
        with self.assertRaisesRegex(RuntimeError, 'Latest backwards'):
            self.publish()
        self.assertEqual(self.client.mutations(), [])


class PackageTests(ReleaseFixture):
    def test_package_is_deterministic_despite_mtime_and_installer_line_endings(self):
        before = [assets.sha256(path) for path in self.assets]
        os.utime(self.binary, (1234567890, 1234567890))
        (self.root / 'deploy/install.sh').write_bytes(b'#!/bin/sh\r\nexit 0\r\n')
        after = assets.package(self.root, self.binary, self.info)
        self.assertEqual([assets.sha256(path) for path in after], before)
        for path in after[1:]:
            self.assertNotIn(b'\r', path.read_bytes(), path.name)

    def test_archive_has_one_executable_binary_and_checksums_cover_assets(self):
        with tarfile.open(self.assets[0], 'r:gz') as bundle:
            members = bundle.getmembers()
            self.assertEqual([(m.name, m.mode) for m in members], [('sub2api', 0o755)])
            self.assertEqual(bundle.extractfile(members[0]).read(), self.binary.read_bytes())
        self.assertEqual(assets.verify(self.root, self.info), self.assets)
        self.assets[1].write_bytes(b'tampered installer')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            assets.verify(self.root, self.info)

    def test_archive_binary_hash_is_verified_beyond_outer_checksums(self):
        self.info['binary_sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'Archived binary differs'):
            assets.package(self.root, self.binary, self.info)

    def test_bad_archive_structure_is_rejected_even_with_valid_outer_hashes(self):
        for members in ([('sub2api', 0o644)], [('other-name', 0o755)],
                        [('sub2api', 0o755), ('extra', 0o755)]):
            with self.subTest(members=members):
                with tarfile.open(self.assets[0], 'w:gz') as bundle:
                    for name, mode in members:
                        member = tarfile.TarInfo(name)
                        member.mode, member.size = mode, self.binary.stat().st_size
                        bundle.addfile(member, io.BytesIO(self.binary.read_bytes()))
                self.assets[-1].write_text(''.join(f'{assets.sha256(p)}  {p.name}\n' for p in self.assets[:-1]), encoding='utf-8')
                with self.assertRaisesRegex(ValueError, 'exactly one executable'):
                    assets.verify(self.root, self.info)

    def test_same_size_remote_digest_mismatch_is_rejected(self):
        candidate = {'id': 1, 'size': self.binary.stat().st_size, 'digest': 'sha256:' + '0' * 64}
        self.assertFalse(github.asset_matches(self.client, candidate, self.binary))
        self.assertEqual(self.client.calls, [])


class VersionAndBuildTests(ReleaseFixture):
    def test_only_local_four_part_versions_are_accepted(self):
        path = self.root / 'VERSION'
        with patch.object(release, 'VERSION_FILE', path):
            for version in ('0.2.8.0', '0.2.8.18', '0.2.8.123'):
                path.write_text(version + '\n', encoding='utf-8')
                self.assertEqual(release.read_version(), version)
            for version in ('2.8.14', '0.2.8', '0.2.8.01', '0.2.9.1', 'v0.2.8.18'):
                with self.subTest(version=version):
                    path.write_text(version, encoding='utf-8')
                    with self.assertRaises(ValueError):
                        release.read_version()

    def test_prepare_increments_once_and_reuses_uncommitted_version(self):
        path = self.root / 'VERSION'
        path.write_text('0.2.8.17\n', encoding='utf-8')
        with patch.object(release, 'VERSION_FILE', path), patch.object(release, 'git', return_value='0.2.8.17'), redirect_stdout(io.StringIO()):
            release.prepare_version()
            release.prepare_version()
        self.assertEqual(path.read_text(encoding='utf-8'), '0.2.8.18\n')

    def test_dirty_publish_stops_before_auth_build_or_network(self):
        state = {'commit': self.info['commit'], 'source_sha256': 'c' * 64, 'source_modified': True}
        with ExitStack() as stack:
            stack.enter_context(patch('sys.argv', ['local_release.py', 'publish']))
            stack.enter_context(patch.object(release, 'read_version', return_value=self.info['version']))
            stack.enter_context(patch.object(release, 'source_state', return_value=state))
            auth = stack.enter_context(patch.object(release, 'access_token'))
            build = stack.enter_context(patch.object(release, 'create_build'))
            with self.assertRaisesRegex(RuntimeError, 'Uncommitted source'):
                release.main()
            auth.assert_not_called()
            build.assert_not_called()

    def test_source_changes_after_build_block_publication(self):
        state = {'commit': self.info['commit'], 'source_sha256': 'c' * 64, 'source_modified': False}
        with ExitStack() as stack:
            stack.enter_context(patch('sys.argv', ['local_release.py', 'publish']))
            stack.enter_context(patch.object(release, 'read_version', return_value=self.info['version']))
            stack.enter_context(patch.object(release, 'source_state', side_effect=[state, dict(state, source_sha256='d' * 64)]))
            stack.enter_context(patch.object(release, 'access_token', return_value='test-token'))
            stack.enter_context(patch.object(release, 'GitHub', return_value=self.client))
            stack.enter_context(patch.object(release, 'create_build', return_value=(self.info, self.assets)))
            publish = stack.enter_context(patch.object(release, 'publish'))
            stack.enter_context(redirect_stdout(io.StringIO()))
            with self.assertRaisesRegex(RuntimeError, 'Source changed after build'):
                release.main()
            publish.assert_not_called()
        self.assertEqual(self.client.mutations(), [])


class AuthenticationTests(unittest.TestCase):
    def test_http_and_connection_errors_do_not_expose_token_or_response_body(self):
        token = 'secret-test-token-never-log'
        failures = [HTTPError('https://api.github.com', 401, token, {}, io.BytesIO(token.encode())),
                    URLError('failed with ' + token)]
        for failure in failures:
            with self.subTest(error=type(failure).__name__), patch.object(github, 'build_opener') as opener:
                if isinstance(failure, HTTPError):
                    self.addCleanup(failure.close)
                opener.return_value.open.side_effect = failure
                output = io.StringIO()
                with redirect_stdout(output), redirect_stderr(output), self.assertRaises(RuntimeError) as raised:
                    github.GitHub(token).request('GET', '/repos/ccx1/sub2api')
                self.assertNotIn(token, str(raised.exception) + output.getvalue())
                if isinstance(failure, HTTPError):
                    self.assertTrue(failure.closed, 'Close HTTP error bodies before raising a sanitized error')


if __name__ == '__main__':
    unittest.main()
