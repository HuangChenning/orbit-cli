"""Public coverage/repair contracts using generated inventory and local Git only.

ORBIT_BIN=/absolute/path/to/orbit python3 -m unittest discover -s tests -p test_synthetic_config.py -v
"""

import copy
import json
import os
import pathlib
import subprocess
import tempfile
import unittest

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[1]


class SyntheticConfigurationContracts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary = pathlib.Path(os.environ.get("ORBIT_BIN", str(ROOT / ("orbit.exe" if os.name == "nt" else "orbit")))).resolve()
        if not cls.binary.is_file():
            raise AssertionError(f"Build Orbit and set ORBIT_BIN: {cls.binary}")

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="orbit-config-contract-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_TERMINAL_PROMPT="0", GIT_ALLOW_PROTOCOL="file",
                        HOME=str(self.root / "home"), USERPROFILE=str(self.root / "home"))
        self.hub = self.root / "hub"
        self.hub.mkdir()
        self.git("init", "--initial-branch=main")
        self.remote = str(self.root / "synthetic-origin.git")
        self.git("remote", "add", "origin", self.remote)
        for name in ("skills/local/SKILL.md", "skills/vendor/SKILL.md", "workflows/review.md",
                     "scripts/run.sh", "commands/build.md"):
            path = self.hub / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("synthetic fixture\n", encoding="utf-8")
        self.git("add", ".")
        self.git("commit", "-m", "synthetic inventory")
        self.axon_path = self.root / "axon.yaml"
        self.vendor_path = self.root / "axon.vendors.yaml"
        self.axon = dict(repo_path=str(self.hub), targets=[
            dict(source=category, destination=str(self.root / "tool" / category), type="directory")
            for category in ("skills", "workflows", "scripts", "commands")])
        self.vendors = dict(vendors=[dict(name="fixture-vendor", repo=str(self.root / "vendor-origin.git"),
                                         ref="main", subdir="package", dest="skills/vendor")])
        self.config = dict(version="1.0", global_cache=str(self.root / "cache"),
                           orbit_repo=dict(remote=self.remote, branch="main", local_path=str(self.root / "mirror")),
                           link_targets={}, dependencies=[])
        self.path = self.root / "orbit.yaml"
        self.save()

    def git(self, *args):
        return subprocess.run(["git", "-c", "user.name=OrbitTests", "-c", "user.email=orbit@example.invalid",
                               "-c", "core.autocrlf=false", "-C", str(self.hub), *args],
                              capture_output=True, text=True, env=self.env, timeout=20, check=True).stdout

    def save(self):
        for path, value in ((self.path, self.config), (self.axon_path, self.axon), (self.vendor_path, self.vendors)):
            path.write_text(yaml.safe_dump(value, sort_keys=False), encoding="utf-8")

    def check(self, *args, success=True, baseline=True):
        command = [str(self.binary), "--config", str(self.path), "check"]
        if baseline:
            command += ["--axon-config", str(self.axon_path), "--vendors", str(self.vendor_path)]
        result = subprocess.run(command + list(args), cwd=self.root, env=self.env,
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return json.loads(result.stdout)

    def repaired(self):
        report = self.check("--fix")
        self.config = yaml.safe_load(self.path.read_text(encoding="utf-8"))
        return report

    def test_coverage_reports_vendor_and_all_hub_categories_without_writing(self):
        before = self.path.read_bytes()
        report = self.check(success=False)
        self.assertEqual(report["status"], "differences")
        self.assertEqual(report["expected_dependencies"], 5)
        self.assertEqual(report["vendor_dependencies"], 1)
        self.assertEqual(len(report["missing_dependencies"]), 5)
        self.assertEqual(len(report["missing_links"]), 4)
        self.assertEqual(len(report["uncovered_files"]), 5)
        self.assertEqual(self.path.read_bytes(), before)
        self.assertFalse((self.root / "cache").exists())
        self.assertFalse((self.root / "mirror").exists())

    def test_repair_preserves_exact_backup_and_second_run_is_idempotent(self):
        before = self.path.read_bytes()
        report = self.repaired()
        self.assertTrue(report["fixed"])
        self.assertEqual(pathlib.Path(report["backup"]).read_bytes(), before)
        after = self.path.read_bytes()
        backups = list(self.root.glob("orbit.yaml.*.bak"))
        again = self.check("--fix")
        self.assertFalse(again["fixed"])
        self.assertIsNone(again["backup"])
        self.assertEqual(self.path.read_bytes(), after)
        self.assertEqual(list(self.root.glob("orbit.yaml.*.bak")), backups)
        self.assertEqual(again["uncovered_files"], [])

    def test_repair_preserves_custom_labels_link_false_and_additional_dependencies(self):
        self.repaired()
        custom = self.config["dependencies"][0]
        custom.update(labels=["team-owned", "custom-label"], vendor=False, link=False)
        self.config["dependencies"].append(dict(name="extra", source=self.remote, ref="main",
                                                subdir="extra", mirror_dir="extras/local", link=False,
                                                labels=["private-selection"]))
        self.config["link_targets"]["skills"] *= 2
        expected = copy.deepcopy(self.config["dependencies"])
        self.save()
        report = self.repaired()
        self.assertTrue(report["fixed"])
        self.assertEqual(self.config["dependencies"], expected)
        self.assertEqual(report["additional_dependencies"], ["extra"])
        self.assertEqual(len(self.config["link_targets"]["skills"]), 1)

    def test_repair_canonicalizes_legacy_refs_and_keeps_labels_last(self):
        self.repaired()
        custom = self.config["dependencies"][0]
        custom["version"] = custom.pop("ref")
        custom["vendor"] = False
        custom["link"] = False
        custom["labels"] = ["skills", "vendor"]
        self.config["link_targets"]["skills"] = []
        self.save()
        self.repaired()
        asset = self.config["dependencies"][0]
        self.assertEqual(asset["ref"], "main")
        self.assertNotIn("version", asset)
        self.assertFalse(asset["vendor"])
        self.assertFalse(asset["link"])
        self.assertEqual(asset["labels"], ["skills", "vendor"])
        keys = list(asset)
        self.assertEqual(keys[-3:], ["vendor", "link", "labels"])
        self.check()

    def test_conflicting_sources_never_get_overwritten_by_repair(self):
        self.repaired()
        self.config["dependencies"][0]["source"] = str(self.root / "different-origin.git")
        self.save()
        before = self.path.read_bytes()
        backups = list(self.root.glob("orbit.yaml.*.bak"))
        report = self.check("--fix", success=False)
        self.assertFalse(report["fixed"])
        self.assertEqual([item["field"] for item in report["conflicts"]], ["source"])
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(list(self.root.glob("orbit.yaml.*.bak")), backups)

    def test_unknown_sources_require_explicit_inventory_and_are_not_invented(self):
        before = self.path.read_bytes()
        report = self.check("--fix", baseline=False, success=False)
        self.assertEqual(report["status"], "invalid")
        self.assertIn("unknown sources", report["error"])
        self.assertEqual(self.path.read_bytes(), before)
        self.assertEqual(list(self.root.glob("*.bak")), [])

    def test_unknown_configuration_fields_fail_without_repair(self):
        self.config["invented_field"] = "invalid"
        self.save()
        before = self.path.read_bytes()
        report = self.check("--fix", success=False)
        self.assertEqual(report["status"], "invalid")
        self.assertIn("invented_field", report["error"])
        self.assertEqual(self.path.read_bytes(), before)

    def test_missing_category_mapping_is_reported_and_repaired_before_operational_use(self):
        self.repaired()
        del self.config["link_targets"]["skills"]
        self.save()
        report = self.check(success=False)
        self.assertEqual(report["missing_dependencies"], [])
        self.assertEqual([item["name"] for item in report["missing_links"]], ["skills"])
        self.repaired()
        self.assertEqual(self.config["link_targets"]["skills"], [str(self.root / "tool/skills")])
        self.check()

    def test_root_skill_file_requires_mapping_and_cannot_be_guessed(self):
        (self.hub / "skills/UNMAPPED.md").write_text("unknown source mapping\n", encoding="utf-8")
        self.git("add", ".")
        self.git("commit", "-m", "unmapped skill root")
        before = self.path.read_bytes()
        report = self.check("--fix", success=False)
        self.assertEqual(report["status"], "invalid")
        self.assertIn("explicit mapping", report["error"])
        self.assertEqual(self.path.read_bytes(), before)

    def test_repository_inventory_reports_missing_and_unconfigured_files_without_writing(self):
        self.repaired()
        (self.hub / "commands/build.md").unlink()
        extra = self.hub / "skills/unconfigured/SKILL.md"
        extra.parent.mkdir()
        extra.write_text("unconfigured\n", encoding="utf-8")
        before = self.path.read_bytes()
        report = self.check("--repo", str(self.hub), success=False)
        self.assertEqual(report["missing_repo_files"], ["commands/build.md"])
        self.assertEqual(report["unconfigured_repo_files"], ["skills/unconfigured/SKILL.md"])
        self.assertEqual(extra.read_text(encoding="utf-8"), "unconfigured\n")
        self.assertEqual(self.path.read_bytes(), before)


if __name__ == "__main__":
    unittest.main()
