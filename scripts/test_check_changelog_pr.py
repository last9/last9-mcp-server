import unittest

from check_changelog_pr import (
    added_bullets,
    unreleased_bullets,
    validate_bullet,
)


class TestChangelogPRCheck(unittest.TestCase):
    def test_reads_only_unreleased_bullets(self):
        content = """# Changelog

## [Unreleased]
### Fixed
- Fixed a sample bug (#44).

## [1.0.0]
- Older change (#20).
"""
        self.assertEqual(
            unreleased_bullets(content),
            ["- Fixed a sample bug (#44)."],
        )

    def test_detects_new_bullet(self):
        previous = ["- Existing change (#10)."]
        current = [
            "- New change (#44).",
            "- Existing change (#10).",
        ]
        self.assertEqual(
            added_bullets(previous, current),
            ["- New change (#44)."],
        )

    def test_ignores_reordered_bullets(self):
        previous = ["- First (#10).", "- Second (#11)."]
        current = ["- Second (#11).", "- First (#10)."]
        self.assertEqual(added_bullets(previous, current), [])

    def test_accepts_correct_pr_number(self):
        self.assertIsNone(validate_bullet("- Fixed a bug (#44).", 44))

    def test_rejects_wrong_pr_number(self):
        self.assertIsNotNone(validate_bullet("- Fixed a bug (#20).", 44))

    def test_accepts_multiple_pr_numbers(self):
        self.assertIsNone(
            validate_bullet("- Updated dependencies (#20, #44).", 44)
        )


if __name__ == "__main__":
    unittest.main()
