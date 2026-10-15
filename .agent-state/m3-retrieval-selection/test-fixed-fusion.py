"""Small independent hand-calculated controls for the new numeric composition."""
import importlib.util
import pathlib
from fractions import Fraction
import unittest

spec = importlib.util.spec_from_file_location('fusion', pathlib.Path(__file__).with_name('fuse-fixed.py'))
fusion = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fusion)


class FixedFusionControls(unittest.TestCase):
    def test_cosine_not_raw_dot_product_and_ascii_tie(self):
        query = fusion.normalized([3, 4], 2)
        docs = {name: fusion.normalized(vector, 2) for name, vector in {'long':[0, 100], 'b':[6, 8], 'a':[3, 4], 'opposite':[-3, -4]}.items()}
        result = fusion.dense_rank(query, docs, 4)
        self.assertEqual([item[0] for item in result], ['a', 'b', 'long', 'opposite'])
        self.assertEqual([round(item[1], 6) for item in result], [1, 1, .8, -1])

    def test_rrf_shared_hit_and_absent_leg(self):
        result = fusion.fuse(['a', 'b'], ['b', 'c'])
        self.assertEqual(result, [('b', Fraction(1, 62)+Fraction(1, 61)), ('a', Fraction(1, 61)), ('c', Fraction(1, 62))])
        self.assertEqual(fusion.fuse(['b', 'a'], []), [('b', Fraction(1, 61)), ('a', Fraction(1, 62))])

    def test_rank_pair_tie_independent_of_leg_order(self):
        expected = [('a', Fraction(123, 3782)), ('b', Fraction(123, 3782))]
        self.assertEqual(fusion.fuse(['b', 'a'], ['a', 'b']), expected)
        self.assertEqual(fusion.fuse(['a', 'b'], ['b', 'a']), expected)

    def test_top20_and_bad_vectors(self):
        self.assertEqual(len(fusion.fuse([f's{i:02}' for i in range(40)], [])), 20)
        for vector, dimensions in [([0, 0], 2), ([1], 2), ([float('nan')], 1), ([True], 1)]:
            with self.assertRaises(ValueError):
                fusion.normalized(vector, dimensions)
        with self.assertRaises(ValueError):
            fusion.fuse(['a', 'a'], [])


if __name__ == '__main__':
    unittest.main()
