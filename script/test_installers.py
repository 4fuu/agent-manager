import hashlib
import os
import pathlib
import platform
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import zipfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
VERSION = "2026.906.0"


def checksum(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp(prefix="agent-manager-installer-"))

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def make_windows_fixture(self):
        fixture = self.tmp / "fixture"
        payload = self.tmp / "payload"
        (payload / "docs").mkdir(parents=True)
        (payload / "docs" / "runtime.md").write_text("fixture\n")
        (payload / "README.md").write_text("fixture\n")
        (payload / "LICENSE").write_text("license fixture\n")
        source = self.tmp / "main.go"
        source.write_text('package main\nimport "fmt"\nfunc main(){fmt.Println("agent-manager 2026.906.0")}\n')
        env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0")
        subprocess.run(["go", "build", "-o", str(payload / "agent-manager.exe"), str(source)], check=True, env=env)
        fixture.mkdir()
        archive = fixture / f"agent-manager-{VERSION}-windows-amd64.zip"
        with zipfile.ZipFile(archive, "w") as z:
            for p in payload.rglob("*"):
                if p.is_file(): z.write(p, p.relative_to(payload))
        (fixture / "LATEST").write_text("v" + VERSION + "\n")
        (fixture / "SHA256SUMS").write_text(f"{checksum(archive)}  {archive.name}\n")
        return fixture

    @unittest.skipUnless(os.name == "nt", "PowerShell installer is Windows-only")
    def test_powershell_install_reinstall_and_failures_preserve_install(self):
        fixture = self.make_windows_fixture()
        install = self.tmp / "install"
        env = dict(os.environ, AGENT_MANAGER_FIXTURE_DIR=str(fixture))
        command = ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(ROOT / "script/install.ps1"), "-InstallDir", str(install)]
        for _ in range(2):
            result = subprocess.run(command, env=env, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
        installed = install / "agent-manager.exe"
        self.assertEqual((install / "LICENSE").read_text(), "license fixture\n")
        before = installed.read_bytes()
        (fixture / "SHA256SUMS").write_text(f"{'0'*64}  agent-manager-{VERSION}-windows-amd64.zip\n")
        result = subprocess.run(command, env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(installed.read_bytes(), before)
        (fixture / "SHA256SUMS").write_text("not a checksum\n")
        result = subprocess.run(command, env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(installed.read_bytes(), before)
        invalid = subprocess.run(command + ["-Version", "1.2.3"], env=env, capture_output=True)
        self.assertNotEqual(invalid.returncode, 0)
        unsupported_env = dict(env, PROCESSOR_ARCHITECTURE="ARM64")
        unsupported = subprocess.run(command, env=unsupported_env, capture_output=True)
        self.assertNotEqual(unsupported.returncode, 0)

    def make_unix_fixture(self, fixture):
        payload = self.tmp / "unix-payload"
        (payload / "docs").mkdir(parents=True)
        (payload / "README.md").write_text("fixture\n")
        exe = payload / "agent-manager"
        exe.write_text(f"#!/bin/sh\necho 'agent-manager {VERSION}'\n")
        exe.chmod(0o755)
        fixture.mkdir()
        arch = "arm64" if platform.machine().lower() in ("arm64", "aarch64") else "amd64"
        system = "darwin" if platform.system() == "Darwin" else "linux"
        archive = fixture / f"agent-manager-{VERSION}-{system}-{arch}.tar.gz"
        with tarfile.open(archive, "w:gz") as tf:
            for name in ("agent-manager", "README.md", "docs"):
                tf.add(payload / name, arcname=name)
        (fixture / "LATEST").write_text("v" + VERSION + "\n")
        (fixture / "SHA256SUMS").write_text(f"{checksum(archive)}  {archive.name}\n")
        return archive

    @unittest.skipUnless(os.name == "posix", "native POSIX test")
    def test_posix_install_reinstall_and_failures_preserve_install(self):
        fixture = self.tmp / "fixture"
        archive = self.make_unix_fixture(fixture)
        install = self.tmp / "bin"
        env = dict(os.environ, AGENT_MANAGER_FIXTURE_DIR=str(fixture))
        command = ["sh", str(ROOT / "script/install.sh"), "--install-dir", str(install)]
        for _ in range(2):
            result = subprocess.run(command, env=env, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
        (install / "agent-manager").write_text("corrupted")
        self.assertEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)
        self.assertIn(VERSION.encode(), subprocess.check_output([str(install / "agent-manager"), "--version"]))
        old_target = os.readlink(install / "agent-manager")
        (fixture / "SHA256SUMS").write_text(f"{'0'*64}  {archive.name}\n")
        self.assertNotEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)
        self.assertEqual(os.readlink(install / "agent-manager"), old_target)
        (fixture / "SHA256SUMS").write_text("malformed\n")
        self.assertNotEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)
        self.assertNotEqual(subprocess.run(command + ["bad"], env=env, capture_output=True).returncode, 0)
        fake_bin = self.tmp / "fake-bin"
        fake_bin.mkdir()
        fake_uname = fake_bin / "uname"
        fake_uname.write_text("#!/bin/sh\necho UnsupportedOS\n")
        fake_uname.chmod(0o755)
        unsupported_env = dict(env, PATH=str(fake_bin) + os.pathsep + env["PATH"])
        unsupported = subprocess.run(command, env=unsupported_env, capture_output=True)
        self.assertNotEqual(unsupported.returncode, 0)
        self.assertIn(b"unsupported target", unsupported.stderr)

    @unittest.skipUnless(os.name == "posix", "native POSIX test")
    def test_latest_redirect_and_download_path(self):
        fixture = self.tmp / "fixture"
        self.make_unix_fixture(fixture)
        fake_bin = self.tmp / "fake-bin"
        fake_bin.mkdir()
        curl = fake_bin / "curl"
        curl.write_text('''#!/bin/sh
set -eu
case "$*" in
  *url_effective*) printf 'https://github.com/4fuu/agent-manager/releases/tag/v2026.906.0'; exit 0;;
esac
out=
while [ "$#" -gt 0 ]; do
  case "$1" in -o) out=$2; shift 2;; *) url=$1; shift;; esac
done
cp "$TEST_RELEASE_DIR/${url##*/}" "$out"
''')
        curl.chmod(0o755)
        install = self.tmp / "bin"
        env = dict(os.environ, TEST_RELEASE_DIR=str(fixture), PATH=str(fake_bin) + os.pathsep + os.environ["PATH"])
        env.pop("AGENT_MANAGER_FIXTURE_DIR", None)
        result = subprocess.run(["sh", str(ROOT / "script/install.sh"), "--install-dir", str(install)], env=env, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(VERSION.encode(), subprocess.check_output([str(install / "agent-manager"), "--version"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
