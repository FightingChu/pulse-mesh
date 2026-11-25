import io
import os
import sys
import unittest
from contextlib import redirect_stderr
from unittest.mock import patch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

import main


class AccessLogTest(unittest.TestCase):
    def render(self, message, enabled="1"):
        output = io.StringIO()
        handler = object.__new__(main.H)
        with patch.dict(os.environ, {"PULSE_ACCESS_LOG": enabled}):
            with patch.object(main.time, "strftime", return_value="2026-11-26T01:00:00"):
                with redirect_stderr(output):
                    handler.log_message("request %s status %d", message, 404)
        return output.getvalue()

    def test_access_log_escapes_control_characters(self):
        for code in (*range(32), *range(127, 160)):
            with self.subTest(code=code):
                text = self.render("before" + chr(code) + "after")
                self.assertEqual(
                    text,
                    "2026-11-26T01:00:00 request before\\x%02xafter status 404\n" % code,
                )

    def test_access_log_preserves_text_and_distinguishes_escape_literals(self):
        self.assertEqual(
            self.render("/电机/50%"),
            "2026-11-26T01:00:00 request /电机/50% status 404\n",
        )
        self.assertNotEqual(self.render("\x1b[2J"), self.render(r"\x1b[2J"))

    def test_access_log_can_be_disabled(self):
        self.assertEqual(self.render("bad\n\x1b[2J", enabled="0"), "")


if __name__ == "__main__":
    unittest.main()
