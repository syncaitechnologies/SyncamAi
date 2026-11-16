import importlib.util
import json
import os
import pathlib
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest import mock

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.client import BackendError  # noqa: E402

HAS_CRYPTO = importlib.util.find_spec("cryptography") is not None
SITE = "22222222-2222-4222-8222-222222222222"


class _PairHandler(BaseHTTPRequestHandler):
    received: list = []
    reply_status = 201

    def log_message(self, *args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        type(self).received.append((self.path, body))
        if type(self).reply_status == 201:
            payload = {"data": {"site_key": "sck_" + "k" * 43, "site_id": SITE, "key_id": "key-1"}}
        else:
            payload = {"error": {"code": "PAIRING_INVALID", "message": "This pairing code is wrong, already used or expired."}}
        data = json.dumps(payload).encode()
        self.send_response(type(self).reply_status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


@unittest.skipUnless(HAS_CRYPTO, "cryptography not installed")
class PairingTest(unittest.TestCase):
    def setUp(self):
        _PairHandler.received = []
        _PairHandler.reply_status = 201
        self.server = HTTPServer(("127.0.0.1", 0), _PairHandler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.base_url = f"http://127.0.0.1:{self.server.server_port}"
        self.directory = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.directory.name)

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.directory.cleanup()

    def test_code_format(self):
        from syncam_ai.site_worker.pairing import normalize_code

        self.assertEqual(normalize_code(" k7q4-xm92 "), "K7Q4XM92")
        with self.assertRaises(ValueError):
            normalize_code("K7Q4")

    def test_keypair_is_p256(self):
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.asymmetric import ec

        from syncam_ai.site_worker.pairing import new_box_keypair

        private_pem, public_pem = new_box_keypair()
        self.assertIn("BEGIN PRIVATE KEY", private_pem)
        public = serialization.load_pem_public_key(public_pem.encode())
        self.assertIsInstance(public.curve, ec.SECP256R1)

    def test_fernet_vault_round_trip_and_tamper(self):
        from syncam_ai.site_worker.vault import BoxVault

        vault = BoxVault(self.root / "box.vault", use_dpapi=False)
        self.assertEqual(vault.load(), {})
        vault.save({"site_key": "sck_secret"})
        self.assertNotIn(b"sck_secret", vault.path.read_bytes(), "secrets are never stored in clear text")
        self.assertEqual(BoxVault(vault.path, use_dpapi=False).load(), {"site_key": "sck_secret"})
        self.assertEqual(vault.update(extra=1), {"site_key": "sck_secret", "extra": 1})
        (self.root / "box.vault.key").unlink()
        with self.assertRaises(ValueError):
            vault.load()
        (self.root / "other.vault").write_bytes(b"garbage")
        with self.assertRaises(ValueError):
            BoxVault(self.root / "other.vault", use_dpapi=False).load()

    @unittest.skipUnless(sys.platform == "win32", "DPAPI is Windows only")
    def test_dpapi_vault_round_trip(self):
        from syncam_ai.site_worker.vault import BoxVault

        vault = BoxVault(self.root / "box.vault")
        vault.save({"site_key": "sck_secret"})
        self.assertNotIn(b"sck_secret", vault.path.read_bytes())
        self.assertFalse((self.root / "box.vault.key").exists(), "DPAPI needs no key file")
        self.assertEqual(vault.load(), {"site_key": "sck_secret"})

    def test_redeem_success_and_clear_error(self):
        from syncam_ai.site_worker.pairing import redeem_pairing_code

        data = redeem_pairing_code(self.base_url, "K7Q4XM92", "PEM", "Office PC")
        self.assertEqual(data["site_id"], SITE)
        path, body = _PairHandler.received[-1]
        self.assertEqual((path, body), ("/v1/site-ingest/pair", {"code": "K7Q4XM92", "public_key": "PEM", "label": "Office PC"}))
        _PairHandler.reply_status = 404
        with self.assertRaises(BackendError) as raised:
            redeem_pairing_code(self.base_url, "K7Q4XM92", "PEM", "Office PC")
        self.assertIn("already used or expired", str(raised.exception))
        self.assertFalse(raised.exception.retryable)

    def test_pair_command_then_config_uses_the_vault(self):
        from syncam_ai.site_worker.__main__ import main
        from syncam_ai.site_worker.config import load_local_config

        config = self.root / "site.json"
        config.write_text(json.dumps({
            "backend_url": self.base_url,
            "cameras": {"33333333-3333-4333-8333-333333333333": "0"},
            "models": {"detector": "d.onnx"},
            "spool_dir": str(self.root / "spool"),
        }), encoding="utf-8")
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("SYNCAM_SITE_KEY", None)
            with self.assertRaises(ValueError) as unpaired:
                load_local_config(config)
            self.assertIn("not paired", str(unpaired.exception))
            self.assertEqual(main(["pair", "--config", str(config), "--code", "k7q4-xm92", "--name", "Office PC"]), 0)
            sent = _PairHandler.received[-1][1]
            self.assertEqual((sent["code"], sent["label"]), ("K7Q4XM92", "Office PC"))
            self.assertIn("BEGIN PUBLIC KEY", sent["public_key"])
            self.assertEqual(load_local_config(config).site_key, "sck_" + "k" * 43)
            self.assertEqual(main(["pair", "--config", str(config), "--code", "k7q4-xm92"]), 1, "an already paired box is not re-paired silently")
            os.environ["SYNCAM_SITE_KEY"] = "sck_" + "e" * 43
            self.assertEqual(load_local_config(config).site_key, "sck_" + "e" * 43, "an explicit key still wins")

    def test_pair_command_reports_a_bad_code(self):
        from syncam_ai.site_worker.__main__ import main

        config = self.root / "site.json"
        config.write_text(json.dumps({"backend_url": self.base_url}), encoding="utf-8")
        _PairHandler.reply_status = 404
        self.assertEqual(main(["pair", "--config", str(config), "--code", "AAAA-BBBB"]), 1)
        self.assertFalse((self.root / "syncam-box.vault").exists(), "nothing is stored when pairing fails")
        self.assertEqual(main(["pair", "--config", str(config), "--code", "short"]), 2)


if __name__ == "__main__":
    unittest.main()
