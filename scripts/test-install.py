#!/usr/bin/env python3
"""Isolated install/rollback/completion checks; no real SSH or credential access."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]

def run(args, *, env=None, ok=True):
    result = subprocess.run(args, cwd=ROOT, env=env, text=True, capture_output=True)
    if ok and result.returncode:
        raise AssertionError(f'{args}: {result.stdout}\n{result.stderr}')
    if not ok and result.returncode == 0:
        raise AssertionError(f'Unexpected success: {args}')
    return result

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def manifest(payload):
    paths = ['bin/janus', 'libexec/janus-keychain', 'share/janus/install.sh']
    (payload / 'SHA256SUMS').write_text(''.join(f'{digest(payload / p)}  {p}\n' for p in paths))

with tempfile.TemporaryDirectory(prefix='janus-install-tests-') as tmp:
    tmp = Path(tmp).resolve()
    prefix = tmp / 'prefix with spaces'
    env = os.environ.copy()
    env['HOME'] = str(tmp / 'home')
    Path(env['HOME']).mkdir()
    base = [str(ROOT / 'install.sh'), '--local', str(ROOT / 'build'), '--prefix', str(prefix)]
    run(base, env=env)
    installed = prefix / 'bin/janus'
    before = digest(installed)
    assert run([str(installed), 'version'], env=env).stdout == run([str(ROOT / 'build/bin/janus'), '--version'], env=env).stdout
    for path in ['share/man/man1/janus.1', 'share/bash-completion/completions/janus', 'share/zsh/site-functions/_janus', 'share/fish/vendor_completions.d/janus.fish', 'libexec/janus-keychain']:
        assert (prefix / path).is_file(), path
    env['MANPATH'] = str(prefix / 'share/man') + ':'
    assert str(prefix / 'share/man/man1/janus.1') in run(['man', '-w', 'janus'], env=env).stdout
    assert 'disconnect' in run(['man', 'janus'], env=env).stdout
    run(['mandoc', '-T', 'ascii', str(prefix / 'share/man/man1/janus.1')], env=env)
    run(base + ['--version', '9.9.9'], env=env, ok=False)
    assert digest(installed) == before
    corrupt = tmp / 'corrupt'
    shutil.copytree(ROOT / 'build', corrupt, ignore=shutil.ignore_patterns('module-cache'))
    (corrupt / 'bin/janus').write_bytes(b'corrupt')
    run([str(ROOT / 'install.sh'), '--local', str(corrupt), '--prefix', str(prefix)], env=env, ok=False)
    assert digest(installed) == before
    # A payload which works while staged but fails at the final destination must roll back.
    broken = tmp / 'broken'
    shutil.copytree(ROOT / 'build', broken, ignore=shutil.ignore_patterns('module-cache'))
    wrapper = '#!/bin/bash\nif [ "$0" = "$JANUS_TEST_FINAL/bin/janus" ]; then exit 23; fi\nexec "$JANUS_TEST_REAL" "$@"\n'
    (broken / 'bin/janus').write_text(wrapper)
    (broken / 'bin/janus').chmod(0o755)
    manifest(broken)
    env['JANUS_TEST_FINAL'] = str(prefix)
    env['JANUS_TEST_REAL'] = str(ROOT / 'build/bin/janus')
    run([str(ROOT / 'install.sh'), '--local', str(broken), '--prefix', str(prefix)], env=env, ok=False)
    assert digest(installed) == before
    retired = prefix / 'libexec/janus-retired-test'
    retired.write_text('owned old helper fixture')
    untouched = prefix / 'libexec/unrelated-test'
    untouched.write_text('must survive')
    ownership = prefix / 'share/janus/files.sha256'
    with ownership.open('a') as file:
        file.write(f'{digest(retired)}  libexec/janus-retired-test\n')
    run(base, env=env)
    assert not retired.exists()
    assert untouched.read_text() == 'must survive'
    assert not list(prefix.glob('.janus-stage.*'))
    registry = Path(env['HOME']) / '.janus/base'
    registry.mkdir(parents=True, mode=0o700)
    profile = {'id': 'terminal:11111111-1111-4111-8111-111111111111', 'name': 'Build Server', 'host': 'example.invalid', 'user': 'builder', 'port': 22, 'persistSeconds': 7200}
    path = registry / 'devices.json'
    path.write_text(json.dumps({'schemaVersion': 1, 'profiles': [profile]}))
    path.chmod(0o600)
    env['PATH'] = str(prefix / 'bin') + ':' + env['PATH']
    completion = str(prefix / 'share/bash-completion/completions/janus')
    script = r'''
source "$JANUS_TEST_COMPLETION"
COMP_WORDS=(janus connect Bu)
COMP_CWORD=2
COMP_LINE='janus connect Bu'
COMP_POINT=${#COMP_LINE}
_janus_complete
printf '%s\n' "${COMPREPLY[@]}"
'''
    env['JANUS_TEST_COMPLETION'] = completion
    result = run(['/bin/bash', '-c', script], env=env)
    assert 'Build' in result.stdout and 'Server' in result.stdout, (result.stdout, result.stderr)
    run(['/bin/zsh', '-n', str(prefix / 'share/zsh/site-functions/_janus')], env=env)
    assert 'disconnect' in run([str(installed), '__complete', 'help', ''], env=env).stdout
    # Non-path destination/name arguments never request file completion.
    for args in [['spawn', ''], ['connect', '--', 'Bu'], ['rename', 'Build Server', '']]:
        assert ':4' in run([str(installed), '__complete', *args], env=env).stdout
    print('PASS: isolated install, manual discovery/render, checksum and version rejection, rollback, Bash candidates, Zsh syntax, completion semantics')
