import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'test-go.py'


class GoSelectionTests(unittest.TestCase):
    def check_fixture(self, body, expected_status, test_filter='.'):
        with tempfile.TemporaryDirectory(prefix='rss-test-selection-') as directory:
            root = Path(directory)
            (root / 'go.mod').write_text('module verificationfixture\n\ngo 1.22\n')
            (root / 'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestFixture(t *testing.T) { ' + body + ' }\n')
            result = subprocess.run([sys.executable, str(SCRIPT), '--filter', test_filter], cwd=root,
                                    capture_output=True, text=True, env={**os.environ, 'GOWORK': 'off'})
            self.assertEqual(expected_status, result.returncode, result.stdout + result.stderr)
            return result.stdout + result.stderr

    def test_all_skipped_is_rejected(self):
        output = self.check_fixture('t.Skip("deliberately skipped")', 1)
        self.assertIn('No non-skipped tests completed', output)

    def test_empty_filter_selection_is_rejected(self):
        output = self.check_fixture('t.Log("ran")', 1, '^NoMatchingTest$')
        self.assertIn('No non-skipped tests completed', output)

    def test_successful_execution_is_accepted(self):
        self.check_fixture('t.Log("ran")', 0)

    def test_real_failure_status_is_preserved(self):
        output = self.check_fixture('t.Fatal("intentional failure")', 1)
        self.assertIn('intentional failure', output)
