#!/usr/bin/env python3
"""Offline controls: synthetic rows/files only, no Go, Docker or live /proc."""
import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import independent
import tcp_diagnostics as diagnostic


ROW = ("tcp", "0100007F:8007", "0200000A:D431", "01", "54321")


class DiagnosticControls(unittest.TestCase):
    def test_original_set_identity_and_one_call(self):
        rows, calls = {ROW}, []
        class Observer:
            def observe(self, value):
                self.value = value
        observer = Observer()
        def original():
            calls.append(True)
            return rows
        self.assertIs(diagnostic.observe_tcp(original, observer)(), rows)
        self.assertIs(observer.value, rows)
        self.assertEqual(calls, [True])
        self.assertEqual(rows, {ROW})

    def test_original_exception_preserved(self):
        failure = OSError("original snapshot unavailable")
        def original():
            raise failure
        with self.assertRaises(OSError) as raised:
            diagnostic.observe_tcp(original, None)()
        self.assertIs(raised.exception, failure)

    def test_diagnostic_failure_does_not_filter(self):
        class Observer:
            def observe(self, rows):
                raise RuntimeError("sensitive endpoint must not be logged")
        rows = {ROW}
        output = io.StringIO()
        with contextlib.redirect_stderr(output):
            self.assertIs(diagnostic.observe_tcp(lambda: rows, Observer())(), rows)
        self.assertEqual(output.getvalue(), "TCP_DIAGNOSTIC_ERROR RuntimeError\n")

    def test_endpoint_categories_and_byte_order(self):
        for encoded, scope in (("0100007F:8007", "loopback"),
                               ("0200000A:8007", "private"),
                               ("08080808:8007", "public"),
                               ("00000000:8007", "unspecified"),
                               ("00000000000000000000000001000000:8007", "loopback")):
            with self.subTest(scope=scope, encoded=encoded):
                self.assertEqual(diagnostic.endpoint(encoded), {"scope": scope, "port": 32775})

    def test_fd_holder_attribution_and_unknown(self):
        with tempfile.TemporaryDirectory() as directory:
            proc = Path(directory)
            for pid in (111, 222):
                fd = proc / str(pid) / "fd"
                fd.mkdir(parents=True)
                (fd / "7").symlink_to("socket:[54321]")
            holders, complete = diagnostic.inode_holders({"54321", "99999", "0"}, {111}, proc)
            self.assertTrue(complete)
            self.assertEqual(holders["54321"], [{"pid": 111, "owned_now": True}, {"pid": 222, "owned_now": False}])
            self.assertEqual(holders["99999"], [])
            self.assertNotIn("0", holders)

    def test_original_samples_change_record_and_privacy(self):
        base = (ROW[0], ROW[1], ROW[2], "02", ROW[4])
        waited = (ROW[0], ROW[1], ROW[2], "06", "0")
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "pg-control.log").write_text(
                "OWNED nonce=control port=32775 PostgreSQL=170008\n"
                "CHILD actual_wait pid=111 state=exit status 0\n"
                "SUPERVISOR actual_driver_wait pid=222 actual=True actual_exit=0\n")
            observer = diagnostic.TCPDiagnostics(output, lambda _: set())
            supplied = [{base}, {ROW}, {ROW}, {waited}, {base}]
            iterator = iter(supplied)
            wrapped = diagnostic.observe_tcp(lambda: next(iterator), observer)
            with patch.object(diagnostic, "inode_holders", return_value=({"54321": [{"pid": 987, "owned_now": False}]}, True)):
                for expected in supplied:
                    self.assertIs(wrapped(), expected)
                observer.finish()
            raw = (output / "tcp-diagnostics.jsonl").read_text()
            records = [json.loads(line) for line in raw.splitlines()]
            self.assertEqual([r["kind"] for r in records], ["baseline", "delta_change", "delta_change", "delta_change", "diagnostic_end"])
            self.assertEqual(records[-1]["samples"], 5)
            self.assertEqual(records[-1]["last_original_sample"]["sample"], 5)
            self.assertEqual(records[-1]["last_delta"], [])
            change = records[1]
            self.assertEqual(change["retired_owned_pid_numbers"], [111, 222])
            self.assertEqual(change["owned_now"], [os.getpid()])
            row = change["rows"][0]
            self.assertTrue(row["fixture_port_match"])
            self.assertTrue(row["baseline_same_inode_endpoint"])
            self.assertEqual(row["baseline_endpoint_states"], ["02"])
            self.assertEqual(row["state"], "ESTABLISHED")
            self.assertEqual(records[2]["rows"][0]["holder_interpretation"], "kernel_time_wait")
            self.assertEqual(records[3]["row_count"], 0)
            self.assertEqual(records[-1]["last_delta_count"], 0)
            for address in ("0100007F", "0200000A", "127.0.0.1", "10.0.0.2"):
                self.assertNotIn(address, raw)

    def test_shared_gate_budget_remains_original(self):
        namespace = independent.load_supervisor()
        self.assertEqual(namespace["budgets"](False), (123, 3))
        self.assertEqual(namespace["SKILL_HTTP_PG"], independent.SELECTOR)
        self.assertEqual(namespace["SKILL_HTTP_CASES"][independent.SELECTOR], independent.CASES)
        source = independent.SUPERVISOR.read_text()
        self.assertIn("tail_deadline = time.monotonic() + 75", source)
        self.assertIn("if empty != 2:\n                code = 1", source)
        self.assertIn("delta = tcp() - baseline", source)

    def test_unwaited_driver_is_not_retired(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "pg-control.log").write_text(
                "OWNED nonce=control port=32775 PostgreSQL=170008\n"
                "CHILD actual_wait pid=111 state=exit status 0\n"
                "SUPERVISOR actual_driver_wait pid=222 actual=False actual_exit=None code=1\n")
            observer = diagnostic.TCPDiagnostics(output, lambda _: set())
            self.assertEqual(observer.fixture(), (32775, [111]))

    def test_diagnostic_error_keeps_last_original_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            observer = diagnostic.TCPDiagnostics(output, lambda _: set())
            observer.observe(set())
            rows = {ROW}
            with patch.object(observer, "fixture", side_effect=OSError("injected log failure")), contextlib.redirect_stderr(io.StringIO()):
                self.assertIs(diagnostic.observe_tcp(lambda: rows, observer)(), rows)
            observer.finish()
            last = json.loads((output / "tcp-diagnostics.jsonl").read_text().splitlines()[-1])
            self.assertEqual(last["last_original_sample"]["sample"], 2)
            self.assertEqual(last["last_delta_count"], 1)
            self.assertEqual(last["last_delta"][0]["inode"], ROW[4])
            self.assertEqual(last["last_delta"][0]["holder_interpretation"], "unknown")


if __name__ == "__main__":
    unittest.main(verbosity=2)
