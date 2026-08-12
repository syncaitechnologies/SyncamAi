import pathlib
import sys
import unittest

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

import syncam_ai  # noqa: E402


class ScaffoldTest(unittest.TestCase):
    def test_runtime_contract(self) -> None:
        self.assertEqual(syncam_ai.RUNTIME, "python3.12")


if __name__ == "__main__":
    unittest.main()
