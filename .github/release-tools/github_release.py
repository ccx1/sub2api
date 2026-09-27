"""Publish verified assets as one complete GitHub Release using the REST API."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

REPOSITORY = 'ccx1/sub2api'
API = 'https://api.github.com'


class SafeRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        if urlparse(newurl).scheme != 'https':
            raise ValueError('Refusing a non-HTTPS GitHub download redirect')
        redirected = super().redirect_request(request, fp, code, msg, headers, newurl)
        if urlparse(request.full_url).netloc != urlparse(newurl).netloc:
            redirected.remove_header('Authorization')
        return redirected


def access_token():
    token = os.environ.get('GH_TOKEN') or os.environ.get('GITHUB_TOKEN')
    if token:
        return token.strip()
    try:
        result = subprocess.run(['gh', 'auth', 'token', '--hostname', 'github.com'],
                                capture_output=True, text=True, timeout=15, check=True)
        return result.stdout.strip()
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError('GitHub authentication required: run gh auth login or set GH_TOKEN locally.') from None


class GitHub:
    def __init__(self, token):
        self.token = token

    def request(self, method, path, *, data=None, content_type='application/json', raw=False):
        url = path if path.startswith('https://') else API + path
        parsed = urlparse(url)
        if parsed.scheme != 'https' or parsed.hostname not in ('api.github.com', 'uploads.github.com'):
            raise ValueError('Unexpected GitHub API host')
        body = data if isinstance(data, bytes) else json.dumps(data).encode() if data is not None else None
        request = Request(url, data=body, method=method, headers={
            'Authorization': 'Bearer ' + self.token,
            'Accept': 'application/octet-stream' if raw else 'application/vnd.github+json',
            'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'sub2api-local-release',
            'Content-Type': content_type})
        try:
            with build_opener(SafeRedirect()).open(request, timeout=180) as response:
                payload = response.read()
                return payload if raw else json.loads(payload) if payload else None
        except HTTPError as error:
            error.close()
            if error.code == 404 and method == 'GET':
                return None
            raise RuntimeError(f'GitHub {method} {parsed.path} failed: HTTP {error.code}; draft retained for retry.') from None
        except URLError as error:
            raise RuntimeError(f'GitHub connection failed ({type(error.reason).__name__}); retry the same version.') from None


def repository_path(suffix):
    return f'/repos/{REPOSITORY}{suffix}'


def preflight(client, commit):
    repository = client.request('GET', repository_path(''))
    if not repository or repository.get('permissions', {}).get('push') is False:
        raise RuntimeError(f'No release write access to {REPOSITORY}.')
    remote = client.request('GET', repository_path('/commits/' + commit))
    if not remote or remote.get('sha') != commit:
        raise RuntimeError('Push the reviewed source commit to origin before publishing.')


def verify_tag(client, tag, commit):
    ref = client.request('GET', repository_path('/git/ref/tags/' + quote(tag, safe='')))
    if not ref:
        return
    obj = ref['object']
    for _ in range(5):
        if obj['type'] == 'commit':
            if obj['sha'] != commit:
                raise RuntimeError('Release tag points to a different source commit; use a new version.')
            return
        if obj['type'] != 'tag':
            break
        obj = client.request('GET', repository_path('/git/tags/' + obj['sha']))['object']
    raise RuntimeError('Unable to resolve release tag to its source commit.')


def find_release(client, tag):
    # 草稿也要查询，避免上传失败重试时创建重复发行版。
    for page in range(1, 21):
        releases = client.request('GET', repository_path(f'/releases?per_page=100&page={page}')) or []
        for release in releases:
            if release['tag_name'] == tag:
                return release
        if len(releases) < 100:
            return None
    raise RuntimeError('Release history exceeds the lookup limit; refusing an ambiguous publication.')


def asset_matches(client, asset, path):
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if asset.get('size') != path.stat().st_size:
        return False
    remote_digest = asset.get('digest')
    if remote_digest:
        return remote_digest == 'sha256:' + digest
    payload = client.request('GET', repository_path('/releases/assets/' + str(asset['id'])),
                             content_type='application/octet-stream', raw=True)
    return isinstance(payload, bytes) and hashlib.sha256(payload).hexdigest() == digest


def release_notes(info):
    tag = 'v' + info['version']
    return (f"平台版本：{info['version']}\n\nRanxi 来源版本：{info['ranxi_version']}\n\n"
            f"源码提交：`{info['commit']}`\n\n目标：Linux amd64，CGO_ENABLED=0，embed，release 构建。\n\n"
            '安装：\n```bash\n'
            f'curl -fsSL https://github.com/{REPOSITORY}/releases/download/{tag}/install.sh | sudo bash -s -- install -v {tag}\n'
            '```\n\n升级保留运行时数据和配置；Ranxi 更新仍需评估并合并源码。\n')


def verify_new_version(client, info):
    if info.get('source_modified'):
        raise RuntimeError('Cannot publish uncommitted source.')
    if not re.fullmatch(r'0\.2\.8\.(0|[1-9][0-9]*)', info['version']):
        raise ValueError('Expected a local four-part release version.')
    latest = client.request('GET', repository_path('/releases/latest'))
    if not latest:
        return
    latest_version = latest['tag_name'].removeprefix('v')
    if re.fullmatch(r'0\.2\.8\.[0-9]+', latest_version):
        if tuple(map(int, info['version'].split('.'))) < tuple(map(int, latest_version.split('.'))):
            raise RuntimeError('A newer platform release already exists; refusing to move Latest backwards.')


def publish(client, info, assets):
    tag = 'v' + info['version']
    verify_new_version(client, info)
    preflight(client, info['commit'])
    verify_tag(client, tag, info['commit'])
    release = find_release(client, tag)
    notes = release_notes(info)
    if release and release.get('draft') and release.get('target_commitish') != info['commit']:
        raise RuntimeError('Existing draft belongs to another source commit; use a new version.')
    if not release:
        release = client.request('POST', repository_path('/releases'), data={
            'tag_name': tag, 'target_commitish': info['commit'], 'name': 'Sub2API ' + tag,
            'body': notes, 'draft': True, 'prerelease': False})
    existing = {asset['name']: asset for asset in release.get('assets', [])}
    expected = {path.name for path in assets}
    if set(existing) - expected:
        raise RuntimeError('Unexpected assets on this release; refusing to alter it.')
    for path in assets:
        asset = existing.get(path.name)
        if asset and release['draft'] and asset.get('state') == 'starter' and asset.get('size') == 0:
            client.request('DELETE', repository_path('/releases/assets/' + str(asset['id'])))
            asset = None
        if asset:
            if not asset_matches(client, asset, path):
                raise RuntimeError(f'Existing asset differs: {path.name}; never overwrite a published version.')
            continue
        if not release['draft']:
            raise RuntimeError('Published release is incomplete; use a new version instead of mutating it.')
        url = release['upload_url'].split('{', 1)[0] + '?name=' + quote(path.name)
        uploaded = client.request('POST', url, data=path.read_bytes(), content_type='application/octet-stream')
        if not asset_matches(client, uploaded, path):
            raise RuntimeError('Uploaded asset verification failed; release remains a draft.')
    verified = client.request('GET', repository_path('/releases/' + str(release['id'])))
    remote_assets = {asset['name']: asset for asset in verified.get('assets', [])}
    if set(remote_assets) != expected or any(not asset_matches(client, remote_assets[p.name], p) for p in assets):
        raise RuntimeError('Release is not complete; publication aborted and draft retained.')
    if verified['draft']:
        verified = client.request('PATCH', repository_path('/releases/' + str(release['id'])),
                                  data={'draft': False, 'make_latest': 'true', 'body': notes})
    return verified['html_url']
