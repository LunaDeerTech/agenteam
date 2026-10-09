// Closed acceptance for the six predeclared Resolve rejection tuples. This
// module consumes safe observations only; it never reads a response or retries.
export function resolveRejectionReady(native: any, owner: any, status: 404 | 409): boolean {
  if (!native || !owner) return false;
  const nativeTrue = ['headers_seen', 'read_done', 'request_id_match', 'content_length_present', 'content_length_valid', 'content_encoding_identity', 'content_length_comparable', 'content_length_matches_eof', 'eof_before_interruption'];
  const nativeFalse = ['cancel_before_eof', 'signal_aborted', 'signal_aborted_at_start'];
  if (!nativeTrue.every(key => native[key] === true) || !nativeFalse.every(key => native[key] === false) || native.failure !== 'none' || native.status !== status || native.requests !== 1 || native.readers !== 1 || native.read_calls < 1 || native.read_calls !== native.read_settled || native.read_rejected !== 0 || native.abort_events !== 0 || native.read_rejected_order !== 0 || native.abort_order !== 0) return false;
  for (const prefix of ['reader_cancel', 'stream_cancel']) if (native[prefix + '_calls'] !== 1 || native[prefix + '_settled'] !== 1 || native[prefix + '_rejected'] !== 0) return false;
  if (native.release_calls !== 1 || native.release_successes !== 1 || native.bytes !== native.content_length || native.bytes <= 0) return false;
  if (!(native.headers_order > 0 && native.read_done_order > native.headers_order && native.reader_cancel_order > native.read_done_order && native.release_order > native.reader_cancel_order && native.stream_cancel_order > native.release_order)) return false;
  const ownerTrue = ['typed_problem', 'problem_schema_valid', 'problem_tuple_matches', 'problem_instance_matches', 'problem_request_id_matches', 'entry_authenticated', 'entry_not_busy', 'entry_identity_matches', 'rejection_authenticated', 'rejection_not_busy', 'rejection_identity_matches', 'rejection_role_matches', 'current_role_matches', 'target_url_at_call', 'target_url_current', 'identity_current', 'authenticated', 'not_busy', 'loading_dom_observed', 'error_dom_after_rejection', 'error_heading_present', 'error_heading_visible', 'zero_provider_lists', 'zero_dialogs'];
  if (!ownerTrue.every(key => owner[key] === true) || ['left_target', 'observer_failed', 'lifetime_expired', 'old_loading_dom_present'].some(key => owner[key] !== false)) return false;
  if (owner.resolve_calls !== 1 || owner.target_calls !== 1 || owner.fulfilled !== 0 || owner.rejected !== 1 || owner.synchronous_throws !== 0 || owner.pending_observations !== 0 || owner.late_events !== 0 || owner.problem_status !== status) return false;
  const t = owner.timing;
  return !!t && ['call', 'loading_dom', 'rejected', 'error_dom', 'sample'].every(key => typeof t[key] === 'number' && Number.isFinite(t[key]) && t[key] >= 0) && t.call <= t.loading_dom && t.loading_dom <= t.rejected && t.rejected <= t.error_dom && t.error_dom <= t.sample;
}

export function acceptedResolveRejection(e: any, status: 404 | 409, deadline: number, now: number): boolean {
  return Number.isFinite(deadline) && now < deadline && !!e && e.request_kind === 'resolve' && e.pw_candidates_before_action === 0 && e.pw_candidates_after_action === 1 && e.pw_target_requests === 1 && e.pw_selected_target_match === true && e.pw_request_match === true && e.pw_request_id_seen === true && e.pw_status === status && ['none', 'aborted'].includes(e.pw_failure) && e.snapshot_source === 'end' && e.snapshot_selected_bound === true && e.slot_end_observed === true && e.samples > 0 && e.sample_settled === e.samples && e.sample_failed === 0 && e.sample_joined === true && e.sample_join_unavailable === false && e.resolve_publication_source === 'end' && e.resolve_publication_install === 'armed' && e.resolve_publication_selected_bound === true && e.resolve_publication_observers_retired === true && e.resolve_listeners_retired === true && e.resolve_final_counters_stable === true && e.resolve_publication?.hooks_retired === true && resolveRejectionReady(e.native, e.resolve_publication, status);
}

// Register the exact public Promise once. A close rejection is an observed
// retirement outcome, never a claim that network response.finished succeeded.
export function resolveFinishedLifetime(page: { close(): Promise<void>; isClosed(): boolean }) {
  const rows: { settled: boolean; result: 'pending' | 'returned-null' | 'returned-error' | 'rejected'; afterClose: boolean; joined: Promise<void> }[] = [];
  const originals = new Set<Promise<unknown>>();
  let closing = false, closed = false, joined = false;
  let tail: Promise<void> | undefined;
  return {
    register<T>(original: Promise<T>): Promise<T> {
      if (closing || rows.length >= 6 || originals.has(original)) throw new Error('PROJECT_MODELS_RESOLVE_FINISHED_REGISTRATION_INVALID');
      originals.add(original);
      const row = { settled: false, result: 'pending' as 'pending' | 'returned-null' | 'returned-error' | 'rejected', afterClose: false, joined: Promise.resolve() };
      row.joined = original.then(value => { row.settled = true; row.result = value === null ? 'returned-null' : 'returned-error'; row.afterClose = closing && page.isClosed(); }, () => { row.settled = true; row.result = 'rejected'; row.afterClose = closing && page.isClosed(); });
      rows.push(row);
      return original;
    },
    closeAndJoin() {
      return tail ??= (async () => {
        closing = true;
        await page.close(); closed = page.isClosed();
        if (!closed) throw new Error('PROJECT_MODELS_RESOLVE_PAGE_CLOSE_INCOMPLETE');
        await Promise.all(rows.map(row => row.joined));
        joined = rows.every(row => row.settled);
        if (!joined) throw new Error('PROJECT_MODELS_RESOLVE_FINISHED_JOIN_INCOMPLETE');
      })();
    },
    facts() { return { registered: rows.length, page_closed: closed, all_original_promises_joined: joined, outcomes: rows.map(({ settled, result, afterClose }) => ({ settled, result, after_close: afterClose })) }; },
  };
}
