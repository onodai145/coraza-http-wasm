import threading
import unittest

from outcome import finalize_status


class FinalOutcomeTest(unittest.TestCase):
    def test_late_monitor_abort_cannot_be_passed(self):
        state = {'status': 'passed', 'reasons': []}
        monitor = threading.Thread(target=lambda: state['reasons'].append('memory threshold'))
        monitor.start()
        monitor.join()
        self.assertEqual('failed', finalize_status(state)['status'])
        self.assertEqual(['memory threshold'], state['reasons'])

    def test_missing_artifacts_cannot_be_passed(self):
        state = {'status': 'passed', 'reasons': [], 'capture_error': 'docker logs failed'}
        self.assertEqual('failed', finalize_status(state)['status'])

    def test_live_monitor_cannot_be_passed(self):
        self.assertEqual('failed', finalize_status({'status': 'passed', 'reasons': []}, True)['status'])

    def test_cleanup_failure_cannot_be_passed(self):
        state = {'status': 'passed', 'reasons': [], 'cleanup_error': 'down failed'}
        self.assertEqual('failed', finalize_status(state)['status'])

    def test_clean_run_remains_passed(self):
        self.assertEqual('passed', finalize_status({'status': 'passed', 'reasons': []})['status'])

    def test_prior_failure_is_never_promoted(self):
        self.assertEqual('failed', finalize_status({'status': 'failed', 'reasons': []})['status'])
