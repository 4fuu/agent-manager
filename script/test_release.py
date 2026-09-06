import struct
import tempfile
from pathlib import Path
import unittest
import zipfile

from release import TARGETS, archive_name, pe_subsystem, read_checksums, validate_version, verify_windows_archive, windows_service_name


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

    def test_windows_helper_name_and_gui_subsystem_validation(self):
        self.assertEqual(windows_service_name("2026.907.0"), "agent-manager-service-2026.907.0.exe")
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "helper.exe"
            data = bytearray(256)
            data[:2] = b"MZ"
            struct.pack_into("<I", data, 0x3C, 0x80)
            data[0x80:0x84] = b"PE\0\0"
            struct.pack_into("<H", data, 0x80 + 4 + 20 + 68, 2)
            path.write_bytes(data)
            self.assertEqual(pe_subsystem(path), 2)
            archive = Path(tmp) / "release.zip"
            with zipfile.ZipFile(archive, "w") as z:
                z.write(path, windows_service_name("2026.907.0"))
            verify_windows_archive(archive, "2026.907.0")
            with zipfile.ZipFile(archive, "w"):
                pass
            with self.assertRaises(ValueError):
                verify_windows_archive(archive, "2026.907.0")
            path.write_bytes(b"not PE")
            with self.assertRaises(ValueError):
                pe_subsystem(path)


if __name__ == "__main__":
    unittest.main()
