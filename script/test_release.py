import tempfile
from pathlib import Path
import unittest

from release import TARGETS, archive_name, read_checksums, validate_version


class ReleaseTests(unittest.TestCase):
    def test_calendar_versions(self):
        for value in ("2026.906.0", "2026.1231.12", "2024.229.0"):
            self.assertEqual(validate_version(value), value)
        for value in ("v2026.906.0", "2026.0906.0", "2026.229.0", "2026.931.0", "2026.906.01", "2026.906.-1"):
            with self.assertRaises(ValueError, msg=value):
                validate_version(value)

    def test_exact_manifest(self):
        v = "2026.906.0"
        lines = [f"{'a'*64}  {archive_name(v, t)}\n" for t in TARGETS]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "SHA256SUMS"
            path.write_text("".join(lines))
            self.assertEqual(len(read_checksums(path, v)), 4)
            for invalid in (lines[:-1], lines + [lines[0]], ["garbage\n"]):
                path.write_text("".join(invalid))
                with self.assertRaises(ValueError):
                    read_checksums(path, v)


if __name__ == "__main__":
    unittest.main()
