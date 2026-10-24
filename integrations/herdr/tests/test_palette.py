# SPDX-License-Identifier: EUPL-1.2
"""Offline numerical checks; reference values are independent of the verifier."""

from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import check_palette as palette


class PaletteTests(unittest.TestCase):
    def test_palette_contrast_and_ciede2000(self):
        measurements = palette.measure_palette()
        with patch.object(palette, "ciede2000", wraps=palette.ciede2000) as difference:
            palette.measure_palette()
        self.assertEqual(difference.call_count, 4 * 15)
        self.assertEqual(set(measurements), {
            "contrast", "normal", "protanopia", "deuteranopia", "tritanopia"
        })
        for mode, (value, colours) in measurements.items():
            self.assertGreaterEqual(value, 4.5 if mode == "contrast" else
                                    8 if mode == "normal" else 6, (mode, colours))
        self.assertTrue(palette.passes(measurements))
        # WCAG's black/white endpoints and the #777777 contrast boundary.
        self.assertEqual(palette.contrast("#000000", "#ffffff"), 21)
        self.assertAlmostEqual(palette.contrast("#777777", "#ffffff"),
                               4.478089453577214, places=10)
        self.assertEqual(palette.contrast("#ffffff", "#ffffff"), 1)

    def test_palette_simulations_use_linear_srgb(self):
        # Published sRGB transfer curve: 0.5 encodes ~0.214 linear light.
        self.assertAlmostEqual(palette.linearize(0.5), 0.21404114048223255)
        self.assertAlmostEqual(palette.linearize(0.04045), 0.0031308049535603713)
        self.assertAlmostEqual(palette.linearize(0.02), 0.0015479876160990713)
        # Basis vectors from Machado's severity-1 table, clipped to gamut.
        expected = {
            "protanopia": (0.152286, 0.114503, 0.0),
            "deuteranopia": (0.367322, 0.280085, 0.0),
            "tritanopia": (1.0, 0.0, 0.004733),
        }
        for mode, channels in expected.items():
            self.assertEqual(palette.simulate((1, 0, 0), mode), channels)
            half_red = palette.simulate((palette.linearize(0.5), 0, 0), mode)
            self.assertAlmostEqual(half_red[2], channels[2] * palette.linearize(0.5))
        other_basis = {
            "protanopia": ((1.0, 0.786281, 0.0), (0.0, 0.099216, 1.0)),
            "deuteranopia": ((0.860646, 0.672501, 0.042940),
                             (0.0, 0.047413, 0.968881)),
            "tritanopia": ((0.0, 0.930809, 0.691367),
                           (0.0, 0.147602, 0.303900)),
        }
        for mode, (green, blue) in other_basis.items():
            self.assertEqual(palette.simulate((0, 1, 0), mode), green)
            self.assertEqual(palette.simulate((0, 0, 1), mode), blue)
        # Observe the pipeline input; gamma-coded #808080 would be ~0.502.
        with patch.object(palette, "simulate", wraps=palette.simulate) as simulation:
            palette.tint_lab("#808080", "deuteranopia")
        self.assertAlmostEqual(simulation.call_args.args[0][0],
                               0.21586050011389926)

    def test_ciede2000_sharma_reference_pairs(self):
        # Sharma/Wu/Dalal supplemental data: blue, zero chroma, hue wrap,
        # near-180-degree discontinuity, lightness and low-lightness cases.
        # https://hajim.rochester.edu/ece/sites/gsharma/ciede2000/dataNprograms/ciede2000testdata.txt
        pairs = [
            ((50, 2.6772, -79.7751), (50, 0, -82.7485), 2.0425),
            ((50, 3.1571, -77.2803), (50, 0, -82.7485), 2.8615),
            ((50, 0, 0), (50, -1, 2), 2.3669),
            ((50, 2.49, -0.001), (50, -2.49, 0.0009), 7.1792),
            ((50, 2.49, -0.001), (50, -2.49, 0.0011), 7.2195),
            ((50, -0.001, 2.49), (50, 0.0011, -2.49), 4.7461),
            ((50, 2.5, 0), (50, 0, -2.5), 4.3065),
            ((50, 2.5, 0), (73, 25, -18), 27.1492),
            ((2.0776, 0.0795, -1.135), (0.9033, -0.0636, -0.5514), 0.9082),
        ]
        for first, second, expected in pairs:
            with self.subTest(first=first, second=second):
                self.assertAlmostEqual(palette.ciede2000(first, second), expected, delta=0.00005)
                self.assertAlmostEqual(palette.ciede2000(second, first), expected, delta=0.00005)
        self.assertEqual(palette.ciede2000((50, 0, 0), (50, 0, 0)), 0)

    def test_lab_d65_endpoints(self):
        for colour, expected in (("#000000", 0), ("#ffffff", 100)):
            lightness, a, b = palette.tint_lab(colour)
            self.assertAlmostEqual(lightness, expected, places=3)
            self.assertAlmostEqual(a, 0, places=2)
            self.assertAlmostEqual(b, 0, places=2)

    def test_rejected_palette_and_host_pairs_fail(self):
        old = ("#3a0d0d", "#0d2238", "#3a2a0d", "#2a0d3a", "#34370d", "#262626")
        measurements = palette.measure_palette(old)
        self.assertLess(measurements["deuteranopia"][0], 3)
        self.assertFalse(palette.passes(measurements))
        # All 15 pairs matter, including the host: duplicate it as a workspace.
        self.assertFalse(palette.passes(palette.measure_palette(
            (palette.HOST_TINT, palette.HOST_TINT, *palette.TINTS[1:]))))
        # A bright tint fails the contrast gate even with separated hue.
        self.assertFalse(palette.passes(palette.measure_palette(
            (palette.HOST_TINT, "#ffffff", *palette.TINTS[1:]))))

    def test_thresholds_are_inclusive_and_each_mode_is_enforced(self):
        values = {"contrast": (4.5, ()), "normal": (8.0, ())}
        values.update({mode: (6.0, ()) for mode in palette.MACHADO})
        self.assertTrue(palette.passes(values))
        for mode, (value, _) in values.items():
            with self.subTest(mode=mode):
                self.assertFalse(palette.passes({**values, mode: (value - 0.000001, ())}))

    def test_cli_reports_measurements_and_fails_for_rejected_constants(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("check_palette.py", "identity.py"):
                shutil.copyfile(Path(palette.__file__).parent / name, root / name)
            env = dict(os.environ)
            env.pop("PYTHONDONTWRITEBYTECODE", None)
            env.pop("PYTHONPYCACHEPREFIX", None)
            result = subprocess.run([sys.executable, "-B", str(root / "check_palette.py")],
                                    capture_output=True, text=True, check=False, env=env)
            self.assertFalse((root / "__pycache__").exists(), "palette child wrote bytecode")
        self.assertEqual(result.returncode, 0, result.stderr)
        for mode in ("contrast", "normal", *palette.MACHADO):
            self.assertIn(mode, result.stdout)
        with patch.object(palette, "TINTS", ("#ffffff",) * 5), redirect_stdout(StringIO()):
            self.assertEqual(palette.main(), 1)


if __name__ == "__main__":
    unittest.main()
