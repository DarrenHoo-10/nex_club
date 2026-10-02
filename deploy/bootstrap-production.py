#!/usr/bin/env python3
"""Provision a fresh Nex Club database using secrets staged on the deployment host.
Run from the production compose directory. Existing runtime credentials are never reset.
"""
import os
import re
import subprocess
from pathlib import Path
from urllib.parse import urlsplit

root = Path.cwd()


def env_file(path):
    values = {}
    for line in path.read_text().splitlines():
        if line and not line.startswith('#'):
            key, value = line.split('=', 1)
            values[key] = value
    return values


def run(args, *, data=None, env=None):
    result = subprocess.run(args, input=data, env=env, text=True, capture_output=True)
    with (root / 'bootstrap.log').open('a') as log:
        log.write(result.stdout + result.stderr)
    (root / 'bootstrap.log').chmod(0o600)
    if result.returncode:
        raise SystemExit('Deployment step failed; inspect the private bootstrap.log on the server')
    return result.stdout.strip()


compose = ['docker', 'compose', '-f', str(root / 'compose.yaml')]
run(compose + ['config', '--quiet'])
run(compose + ['up', '-d', '--wait', 'postgres'])
print('PostgreSQL healthy', flush=True)
run(compose + ['run', '--rm', 'migrate'])
print('Application and job migrations applied', flush=True)
psql = compose + ['exec', '-T', 'postgres', 'psql', '-U', 'nex_owner', '-d', 'nex_club', '-v', 'ON_ERROR_STOP=1', '-At']
password = urlsplit(env_file(root / 'secrets/runtime.env')['NEX_DATABASE_URL']).password
if not password or not re.fullmatch(r'[0-9a-f]{48}', password):
    raise SystemExit('Expected a freshly generated hexadecimal runtime password')
exists = run(psql, data="SELECT count(*) FROM pg_roles WHERE rolname='nex_runtime';")
if exists == '0':
    run(psql, data=f"CREATE ROLE nex_runtime LOGIN PASSWORD '{password}';\nGRANT nex_app TO nex_runtime;\n")
elif exists != '1':
    raise SystemExit('Unexpected runtime role state')
print('Restricted application login ready', flush=True)
if run(psql, data='SELECT count(*) FROM admin_users;') == '0':
    values = env_file(root / 'secrets/admin.env')
    run(compose + ['run', '--rm', '--no-deps', '-e', 'NEX_ADMIN_USERNAME', '-e', 'NEX_ADMIN_PASSWORD', 'admin', 'admin', 'init'], env={**os.environ, **values})
print('Administrator initialized', flush=True)
run(compose + ['up', '-d', '--wait', 'api', 'worker'])
print('API and worker running; API readiness passed', flush=True)
