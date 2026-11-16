#!/usr/bin/env python3
"""独立 bootstrap 归档的内容、权限和目标保护专项。"""

import contextlib
import importlib.util
import io
import pathlib
import tarfile
import tempfile
import unittest


DEPLOY = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('mx_bootstrap_package_tested', DEPLOY / 'package-update-host.py')
p = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(p)
EXPECTED = {
    'mx-update.py': 0o700,
    'mx-update-host.py': 0o700,
    'mx-update-host-install.py': 0o700,
    'mx-update-host.service': 0o644,
    'mx-update-operation@.service': 0o644,
    'README.update-host.zh-CN.md': 0o644,
}


class BootstrapPackageTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='mx-bootstrap-package-')
        self.addCleanup(temporary.cleanup)
        self.root = pathlib.Path(temporary.name)
        self.output = self.root / 'bootstrap.tar.gz'

    def test_archive_contains_exact_local_material_bytes(self):
        self.assertEqual(p.package(self.output), self.output)
        with tarfile.open(self.output, 'r:gz') as archive:
            members = archive.getmembers()
            self.assertEqual(len(members), len(EXPECTED))
            self.assertEqual(set(archive.getnames()), set(EXPECTED))
            self.assertNotIn('release.json', archive.getnames())
            for member in members:
                self.assertTrue(member.isfile())
                self.assertEqual(archive.extractfile(member).read(), (DEPLOY / member.name).read_bytes())

    def test_archive_modes_and_root_ownership_are_explicit(self):
        p.package(self.output)
        with tarfile.open(self.output, 'r:gz') as archive:
            for member in archive.getmembers():
                with self.subTest(name=member.name):
                    self.assertEqual(member.mode, EXPECTED[member.name])
                    self.assertEqual((member.uid, member.gid), (0, 0))
                    self.assertEqual((member.uname, member.gname), ('root', 'root'))

    def test_existing_output_is_rejected_and_original_bytes_remain(self):
        original = b'existing-bootstrap-archive\n'
        self.output.write_bytes(original)
        messages = io.StringIO()
        with contextlib.redirect_stderr(messages):
            result = p.main(['--output', str(self.output)])
        self.assertEqual(result, 1)
        self.assertIn('输出目标已存在', messages.getvalue())
        self.assertEqual(self.output.read_bytes(), original)
        self.assertEqual(list(self.root.iterdir()), [self.output])


if __name__ == '__main__':
    unittest.main(verbosity=2)
