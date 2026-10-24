import json
import tempfile
import unittest
from pathlib import Path

import libtorrent as lt

from tarbit.config import Config
from tarbit.trackers import BUILTIN, TrackerPool, merge, parse_list
from tarbit.update import newer

MAGNET = "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Test&tr=udp%3A%2F%2Ftracker.example.com%3A80"


class VersionTest(unittest.TestCase):
    def test_newer(self):
        self.assertTrue(newer("v0.1.1", "0.1.0"))
        self.assertTrue(newer("v1.0", "0.9.9"))
        self.assertTrue(newer("0.2.0", "0.1.10"))
        self.assertFalse(newer("v0.1.0", "0.1.0"))
        self.assertFalse(newer("v0.1", "0.1.0"))
        self.assertFalse(newer("v0.0.9", "0.1.0"))
        self.assertFalse(newer("garbage", "0.1.0"))
        self.assertFalse(newer("v0.2.0-beta", "0.2.0"))


class TrackersTest(unittest.TestCase):
    def test_parse_list_skips_junk(self):
        text = "udp://a.example:1/announce\n\n# comment\nwss://b.example/announce\nhttp://c.example/announce\n"
        self.assertEqual(parse_list(text), ["udp://a.example:1/announce", "http://c.example/announce"])

    def test_merge_dedup_case_and_slash(self):
        self.assertEqual(merge(["udp://A.example:1/"], ["udp://a.example:1"]), ["udp://A.example:1/"])

    def test_pool_custom_first_and_builtin_fallback(self):
        with tempfile.TemporaryDirectory() as d:
            pool = TrackerPool(Path(d) / "trackers.json")
            got = pool.get("udp://mine.example:6969/announce")
            self.assertEqual(got[0], "udp://mine.example:6969/announce")
            self.assertIn(BUILTIN[0], got)
            self.assertTrue(pool.stale())

    def test_pool_reads_cache(self):
        with tempfile.TemporaryDirectory() as d:
            f = Path(d) / "trackers.json"
            f.write_text(json.dumps({"updated": 1e12, "trackers": ["udp://cached.example:1/announce"]}))
            pool = TrackerPool(f)
            self.assertFalse(pool.stale())
            self.assertEqual(pool.get()[0], "udp://cached.example:1/announce")


class ConfigTest(unittest.TestCase):
    def test_defaults_port_and_coercion(self):
        with tempfile.TemporaryDirectory() as d:
            c = Config(Path(d) / "settings.json")
            self.assertTrue(20000 <= c["listen_port"] <= 60000)
            changed = c.update({"dl_limit": "500", "theme": "nope", "listen_port": 80, "unknown": 1, "dht": 0})
            self.assertEqual(changed, {"dl_limit": 500, "dht": False})
            again = Config(Path(d) / "settings.json")
            self.assertEqual(again["dl_limit"], 500)
            self.assertFalse(again["dht"])


class EngineParseTest(unittest.TestCase):
    def test_describe_magnet(self):
        from tarbit.engine import Engine
        atp = Engine.parse_source(magnet=MAGNET)
        d = Engine.describe(atp)
        self.assertEqual(d["id"], "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c")
        self.assertEqual(d["name"], "Test")
        self.assertFalse(d["has_metadata"])

    def test_describe_torrent_file(self):
        from tarbit.engine import Engine
        with tempfile.TemporaryDirectory() as d:
            src = Path(d) / "pack"
            src.mkdir()
            (src / "a.txt").write_bytes(b"a" * 1000)
            (src / "b.bin").write_bytes(b"b" * 5000)
            fs = lt.file_storage()
            lt.add_files(fs, str(src))
            t = lt.create_torrent(fs)
            lt.set_piece_hashes(t, d)
            t.set_priv(True)
            data = lt.bencode(t.generate())
            info = Engine.describe(Engine.parse_source(data=data))
            self.assertTrue(info["has_metadata"])
            self.assertTrue(info["private"])
            self.assertEqual(info["size"], 6000)
            self.assertEqual(sorted(f["size"] for f in info["files"]), [1000, 5000])


if __name__ == "__main__":
    unittest.main()
