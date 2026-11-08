"""Finalize benchmark outcomes after monitoring and artifact collection stop."""


def finalize_status(state, monitor_alive=False):
    if monitor_alive:
        state['monitor_error'] = 'resource monitor did not terminate'
    if state.get('reasons') or any(state.get(key) for key in
                                  ('capture_error', 'cleanup_error', 'monitor_error')):
        state['status'] = 'failed'
    return state
