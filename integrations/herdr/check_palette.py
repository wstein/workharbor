#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Offline FR-021a measurements, using only Python's standard library.

Machado severity-1 matrices operate on linear sRGB; simulated channels are
clipped to the display gamut before conversion to CIELAB with a D65 white.
CIEDE2000 uses unit parametric factors (reference viewing conditions).

Sources:
https://www.w3.org/TR/WCAG22/#dfn-relative-luminance
https://www.w3.org/TR/css-color-4/#color-conversion-code
https://www.inf.ufrgs.br/~oliveira/pubs_files/CVD_Simulation/CVD_Simulation.html
https://hajim.rochester.edu/ece/sites/gsharma/ciede2000/ciede2000noteCRNA.pdf

These simulations approximate colour vision; they are no accessibility guarantee.
Five workspace tints necessarily collide beyond five workspaces (and may collide
sooner under the hash). Full title/label text remains the identity cue.
"""

from itertools import combinations
from math import atan2, cos, degrees, exp, hypot, radians, sin, sqrt

from identity import HOST_TINT, TINTS

# Original authors' precomputed table, severity 1.0 (Machado et al., 2009).
MACHADO = {
    "protanopia": (
        (0.152286, 1.052583, -0.204868),
        (0.114503, 0.786281, 0.099216),
        (-0.003882, -0.048116, 1.051998),
    ),
    "deuteranopia": (
        (0.367322, 0.860646, -0.227968),
        (0.280085, 0.672501, 0.047413),
        (-0.011820, 0.042940, 0.968881),
    ),
    "tritanopia": (
        (1.255528, -0.076749, -0.178779),
        (-0.078411, 0.930809, 0.147602),
        (0.004733, 0.691367, 0.303900),
    ),
}


def linearize(channel):
    """Decode a normalized sRGB channel to linear light."""
    return channel / 12.92 if channel <= 0.04045 else ((channel + 0.055) / 1.055) ** 2.4


def linear_rgb(colour):
    return tuple(linearize(int(colour[i:i + 2], 16) / 255) for i in (1, 3, 5))


def contrast(first, second):
    """WCAG relative-luminance contrast ratio (unsimulated display)."""
    def luminance(colour):
        return sum(c * w for c, w in zip(linear_rgb(colour), (0.2126, 0.7152, 0.0722)))

    low, high = sorted((luminance(first), luminance(second)))
    return (high + 0.05) / (low + 0.05)


def simulate(rgb, mode):
    """Transform *linear* sRGB, clipping only after matrix multiplication."""
    return tuple(max(0.0, min(1.0, sum(c * w for c, w in zip(rgb, row))))
                 for row in MACHADO[mode])


def tint_lab(colour, mode="normal"):
    rgb = linear_rgb(colour)
    if mode != "normal":
        rgb = simulate(rgb, mode)
    # sRGB primaries -> XYZ D65 (CSS Color 4 reference matrix).
    matrix = (
        (0.4123907992659595, 0.357584339383878, 0.1804807884018343),
        (0.21263900587151036, 0.7151686787677559, 0.07219231536073371),
        (0.01933081871559185, 0.11919477979462598, 0.9505321522496606),
    )
    xyz = [sum(c * w for c, w in zip(rgb, row)) for row in matrix]
    white = (0.3127 / 0.3290, 1.0, (1 - 0.3127 - 0.3290) / 0.3290)

    def lab_curve(value):
        return value ** (1 / 3) if value > 216 / 24389 else (24389 / 27 * value + 16) / 116

    x, y, z = [lab_curve(value / reference) for value, reference in zip(xyz, white)]
    return (116 * y - 16, 500 * (x - y), 200 * (y - z))


def ciede2000(first, second):
    """Sharma/Wu/Dalal equations 2–22, with kL = kC = kH = 1."""
    l1, a1, b1 = first
    l2, a2, b2 = second
    mean_c = (hypot(a1, b1) + hypot(a2, b2)) / 2
    g = (1 - sqrt(mean_c ** 7 / (mean_c ** 7 + 25 ** 7))) / 2
    a1, a2 = (1 + g) * a1, (1 + g) * a2
    c1, c2 = hypot(a1, b1), hypot(a2, b2)
    h1, h2 = degrees(atan2(b1, a1)) % 360, degrees(atan2(b2, a2)) % 360
    dl, dc = l2 - l1, c2 - c1
    dh = h2 - h1
    if c1 * c2 == 0:
        dh = 0
    elif dh > 180:
        dh -= 360
    elif dh < -180:
        dh += 360
    dh_term = 2 * sqrt(c1 * c2) * sin(radians(dh / 2))
    mean_l, mean_c = (l1 + l2) / 2, (c1 + c2) / 2
    if c1 * c2 == 0:
        mean_h = h1 + h2
    elif abs(h1 - h2) <= 180:
        mean_h = (h1 + h2) / 2
    elif h1 + h2 < 360:
        mean_h = (h1 + h2 + 360) / 2
    else:
        mean_h = (h1 + h2 - 360) / 2
    t = (1 - 0.17 * cos(radians(mean_h - 30)) + 0.24 * cos(radians(2 * mean_h))
         + 0.32 * cos(radians(3 * mean_h + 6)) - 0.20 * cos(radians(4 * mean_h - 63)))
    sl = 1 + 0.015 * (mean_l - 50) ** 2 / sqrt(20 + (mean_l - 50) ** 2)
    sc, sh = 1 + 0.045 * mean_c, 1 + 0.015 * mean_c * t
    rotation = (-2 * sqrt(mean_c ** 7 / (mean_c ** 7 + 25 ** 7))
                * sin(radians(60 * exp(-((mean_h - 275) / 25) ** 2))))
    return sqrt((dl / sl) ** 2 + (dc / sc) ** 2 + (dh_term / sh) ** 2
                + rotation * (dc / sc) * (dh_term / sh))


def measure_palette(colours=None):
    """Return each minimum and the tint(s) responsible; include all 15 pairs."""
    if colours is None:
        colours = (HOST_TINT, *TINTS)
    results = {"contrast": min((contrast(colour, "#c0c0c0"), (colour,))
                               for colour in colours)}
    for mode in ("normal", *MACHADO):
        labs = [tint_lab(colour, mode) for colour in colours]
        results[mode] = min((ciede2000(labs[i], labs[j]), (colours[i], colours[j]))
                            for i, j in combinations(range(len(colours)), 2))
    return results


def passes(measurements):
    return all(measurements[mode][0] >= threshold for mode, threshold in
               (("contrast", 4.5), ("normal", 8), *((mode, 6) for mode in MACHADO)))


def main():
    measurements = measure_palette()
    for mode, (value, colours) in measurements.items():
        print(f"{mode}: {value:.4f} ({', '.join(colours)})")
    passed = passes(measurements)
    print("PASS" if passed else "FAIL")
    print("Approximation only; five workspace tints; no accessibility guarantee.")
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
