#!/usr/bin/env python3
"""Fail a focused Go test run when no tests execute."""
import argparse
import json
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--packages', default='./...')
parser.add_argument('--filter', default='.')
args = parser.parse_args()
process = subprocess.Popen(['go', 'test', '-json', '-count=1', '-timeout=100s', '-run', args.filter, *args.packages.split()],
                           stdout=subprocess.PIPE, text=True)
executed = 0
output = {}
for line in process.stdout:
    event = json.loads(line)
    if event.get('Action') == 'pass' and event.get('Test'):
        executed += 1
    package = event.get('Package', 'go')
    if event.get('Output'):
        output.setdefault(package, []).append(event['Output'])
    if event.get('Action') in ('pass', 'fail', 'skip') and not event.get('Test'):
        if event['Action'] == 'fail':
            print(''.join(output.get(package, [])), end='')
        else:
            print(f"{event['Action']}: {package}")
status = process.wait()
if status:
    raise SystemExit(status)
if executed == 0:
    raise SystemExit('No non-skipped tests completed. Check GO_PACKAGES and GO_TEST_FILTER.')
print(f'{executed} non-skipped test and subtest completions observed.')
