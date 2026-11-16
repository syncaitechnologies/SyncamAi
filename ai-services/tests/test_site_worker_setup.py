import importlib.util
import pathlib
import socket
import sys
import tempfile
import threading
import unittest
import uuid

SRC = pathlib.Path(__file__).parents[1] / "src"
sys.path.insert(0, str(SRC))

from syncam_ai.site_worker.brands import guess_family, rtsp_url  # noqa: E402
from syncam_ai.site_worker.capture import StreamSet  # noqa: E402
from syncam_ai.site_worker.config import parse_local_config  # noqa: E402
from syncam_ai.site_worker.discovery import (  # noqa: E402
    FoundDevice,
    ProbeResult,
    RtspReply,
    digest_authorization,
    parse_probe_match,
    probe_channels,
    rtsp_request,
)
from syncam_ai.site_worker.live import LiveRelay  # noqa: E402

HAS_CRYPTO = importlib.util.find_spec("cryptography") is not None
CAMERA = "33333333-3333-4333-8333-333333333333"

# Shared with frontend/apps/web/src/setup-crypto.test.ts: test-only keys
# derived from fixed numbers, so both implementations agree byte for byte.
BOX_SCALAR = 0x5EC0_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0001
EPHEMERAL_SCALAR = 0xE9E0_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0000_0002
VECTOR_ENVELOPE = {
    "v": 1,
    "epk": "BC0S0j2xooqqOuDcBtGTikGu/yl59nEMN2/mrkfIMvsFZ0ojzTmmMJRlFJVNxZkKMENMDzlCK/eaqf0nUys7kiw=",
    "salt": "AAECAwQFBgcICQoLDA0ODw==",
    "iv": "ZGVmZ2hpamtsbW5v",
    "ct": "cjpxB9qinL5xD8T/FBt6msUcQK2+WOG/rVIZ46PHjBCfeEjCSeS3ydzSZJrznponsGEOuolc2Tf5uNI4tYFM",
}
VECTOR_CREDENTIALS = {"username": "syncam", "password": "Pa:ss@w/rd#1"}


def _pem_pair(scalar):
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ec

    key = ec.derive_private_key(scalar, ec.SECP256R1())
    private = key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode()
    public = key.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode()
    return private, public


class BrandTest(unittest.TestCase):
    def test_guess_family(self):
        self.assertEqual(guess_family(["Dahua Rtsp Server/3.0"], []), "dahua")
        self.assertEqual(guess_family(["onvif://www.onvif.org/name/LOREX"], []), "dahua")
        self.assertEqual(guess_family(["", "HIKVISION DS-7608"], []), "hikvision")
        self.assertEqual(guess_family([""], [554, 37777]), "dahua")
        self.assertEqual(guess_family([""], [554, 8000]), "hikvision")
        self.assertIsNone(guess_family(["GStreamer"], [554]))

    def test_rtsp_urls_encode_any_password(self):
        self.assertEqual(rtsp_url("192.168.1.108", 554, "dahua", 3, "syncam", "p@ss:w/rd#"),
                         "rtsp://syncam:p%40ss%3Aw%2Frd%23@192.168.1.108:554/cam/realmonitor?channel=3&subtype=1")
        self.assertEqual(rtsp_url("10.0.0.5", 554, "hikvision", 12), "rtsp://10.0.0.5:554/Streaming/Channels/1202")
        self.assertTrue(rtsp_url("10.0.0.5", 554, "reolink", 2).endswith("/h264Preview_02_sub"))
        self.assertTrue(rtsp_url("10.0.0.5", 554, "dahua", 1, stream="main").endswith("subtype=0"))


class OnvifTest(unittest.TestCase):
    def test_parse_probe_match(self):
        payload = (
            '<env:Body><d:ProbeMatches><d:ProbeMatch><d:XAddrs>http://192.168.1.108/onvif/device_service http://[fe80::1]/onvif</d:XAddrs>'
            "<d:Scopes>onvif://www.onvif.org/name/LOREX onvif://www.onvif.org/hardware/D871A8B</d:Scopes></d:ProbeMatch></d:ProbeMatches></env:Body>"
        )
        xaddrs, scopes = parse_probe_match(payload)
        self.assertEqual(xaddrs[0], "http://192.168.1.108/onvif/device_service")
        self.assertIn("hardware/D871A8B", scopes)
        self.assertEqual(FoundDevice(ip="192.168.1.108", family="dahua").as_dict()["brand"], "Dahua / Lorex / Amcrest / CP Plus")


class _FakeRtspServer:
    """Answers DESCRIBE with a Digest challenge, then 200 for the right password."""

    def __init__(self, username="syncam", password="secret", channels=(1, 2)):
        self.username, self.password, self.channels = username, password, set(channels)
        self.sock = socket.socket()
        self.sock.bind(("127.0.0.1", 0))
        self.sock.listen(16)
        self.port = self.sock.getsockname()[1]
        self.requests = []
        threading.Thread(target=self._serve, daemon=True).start()

    def _serve(self):
        while True:
            try:
                connection, _ = self.sock.accept()
            except OSError:
                return
            with connection:
                request = connection.recv(65536).decode()
                self.requests.append(request)
                first = request.split("\r\n")[0]
                method, uri = first.split()[0], first.split()[1]
                authorization = next((line.split(":", 1)[1].strip() for line in request.split("\r\n") if line.lower().startswith("authorization:")), "")
                if not authorization:
                    reply = 'RTSP/1.0 401 Unauthorized\r\nCSeq: 1\r\nWWW-Authenticate: Digest realm="Login to DVR", nonce="abc123"\r\n\r\n'
                else:
                    expected = digest_authorization('Digest realm="Login to DVR", nonce="abc123"', method, uri, self.username, self.password)
                    good = authorization == expected
                    channel = int(uri.split("channel=")[1].split("&")[0]) if "channel=" in uri else 0
                    status = "200 OK" if good and channel in self.channels else ("401 Unauthorized" if not good else "404 Not Found")
                    reply = f"RTSP/1.0 {status}\r\nCSeq: 2\r\nServer: Dahua Rtsp Server/3.0\r\n\r\n"
                connection.sendall(reply.encode())

    def close(self):
        self.sock.close()


class RtspTest(unittest.TestCase):
    def setUp(self):
        self.server = _FakeRtspServer()

    def tearDown(self):
        self.server.close()

    def test_digest_auth_against_a_recorder(self):
        ok = rtsp_request("127.0.0.1", self.server.port, "DESCRIBE", "/cam/realmonitor?channel=1&subtype=1", "syncam", "secret")
        self.assertEqual((ok.status, ok.headers.get("server")), (200, "Dahua Rtsp Server/3.0"))
        wrong = rtsp_request("127.0.0.1", self.server.port, "DESCRIBE", "/cam/realmonitor?channel=1&subtype=1", "syncam", "nope")
        self.assertEqual(wrong.status, 401)
        anonymous = rtsp_request("127.0.0.1", self.server.port, "OPTIONS")
        self.assertEqual(anonymous.status, 401, "no credentials: the challenge is returned, not answered")

    def test_probe_finds_channels_and_stops_on_wrong_password(self):
        shots = []
        result = probe_channels("127.0.0.1", self.server.port, "syncam", "secret", family="dahua", thumbnail=lambda url: shots.append(url) or {"thumbnail": "x"})
        self.assertEqual((result.family, [item["channel"] for item in result.channels], result.error), ("dahua", [1, 2], ""))
        self.assertIn("syncam:secret@127.0.0.1", shots[0])
        before = len(self.server.requests)
        locked = probe_channels("127.0.0.1", self.server.port, "syncam", "wrong", family="dahua")
        self.assertIn("password", locked.error)
        self.assertLessEqual(len(self.server.requests) - before, 2, "a wrong password is tried once, not per channel")


class ProbeLogicTest(unittest.TestCase):
    def fake(self, answers):
        def request(host, port, method, path, username, password):
            for fragment, status in answers.items():
                if fragment in path:
                    return RtspReply(status, {})
            return RtspReply(404, {})
        return request

    def test_unknown_brand_is_found_by_trying_templates(self):
        result = probe_channels("10.0.0.5", 554, "u", "p", request=self.fake({"Channels/102": 200, "Channels/202": 200}))
        self.assertEqual((result.family, [item["channel"] for item in result.channels]), ("hikvision", [1, 2]))

    def test_unreachable_and_unknown_formats_explain_themselves(self):
        def down(*args):
            raise ConnectionRefusedError()
        self.assertIn("Could not reach RTSP", probe_channels("10.0.0.5", 554, "u", "p", request=down).error)
        self.assertIn("no known RTSP address", probe_channels("10.0.0.5", 554, "u", "p", request=self.fake({})).error)

    def test_stops_after_consecutive_missing_channels(self):
        calls = []

        def request(host, port, method, path, username, password):
            calls.append(path)
            return RtspReply(200 if "channel=1&" in path or "channel=3&" in path else 404, {})
        result = probe_channels("10.0.0.5", 554, "u", "p", family="dahua", request=request)
        self.assertEqual([item["channel"] for item in result.channels], [1, 3])
        self.assertLess(len(calls), 12, "probing stops after four empty channels in a row")


@unittest.skipUnless(HAS_CRYPTO, "cryptography not installed")
class CredentialCryptoTest(unittest.TestCase):
    def test_round_trip_and_cross_language_vector(self):
        from syncam_ai.site_worker.setup_crypto import decrypt_credentials, encrypt_credentials

        box_private, box_public = _pem_pair(BOX_SCALAR)
        self.assertEqual(decrypt_credentials(box_private, encrypt_credentials(box_public, VECTOR_CREDENTIALS)), VECTOR_CREDENTIALS)
        self.assertEqual(decrypt_credentials(box_private, VECTOR_ENVELOPE), VECTOR_CREDENTIALS, "the browser test vector opens in Python")

        from cryptography.hazmat.primitives.asymmetric import ec
        import base64
        fixed = encrypt_credentials(box_public, VECTOR_CREDENTIALS, ephemeral_private=ec.derive_private_key(EPHEMERAL_SCALAR, ec.SECP256R1()),
                                    salt=bytes(range(16)), iv=bytes(range(100, 112)))
        self.assertEqual(fixed, VECTOR_ENVELOPE)
        self.assertEqual(base64.b64decode(fixed["iv"]), bytes(range(100, 112)))

    def test_only_the_addressed_box_can_open_it(self):
        from syncam_ai.site_worker.setup_crypto import decrypt_credentials

        other_private, _ = _pem_pair(0x1234)
        with self.assertRaises(ValueError):
            decrypt_credentials(other_private, VECTOR_ENVELOPE)
        box_private, _ = _pem_pair(BOX_SCALAR)
        tampered = dict(VECTOR_ENVELOPE, ct=VECTOR_ENVELOPE["ct"][:-4] + "AAAA")
        with self.assertRaises(ValueError):
            decrypt_credentials(box_private, tampered)
        for broken in ({}, dict(VECTOR_ENVELOPE, v=2), dict(VECTOR_ENVELOPE, iv="AAAA"), dict(VECTOR_ENVELOPE, epk="***")):
            with self.assertRaises(ValueError):
                decrypt_credentials(box_private, broken)


class _Backend:
    def __init__(self):
        self.results = {}
        self.tasks = []

    def post_setup_result(self, task_id, body):
        self.results[task_id] = body

    def poll(self):
        return {"cameras": [], "tasks": self.tasks}


class _Stream:
    def __init__(self, camera_id, source):
        self.camera_id, self.source, self.started = camera_id, source, False

    def start(self):
        self.started = True


@unittest.skipUnless(HAS_CRYPTO, "cryptography not installed")
class SetupAgentTest(unittest.TestCase):
    def setUp(self):
        from syncam_ai.site_worker.vault import BoxVault

        self.directory = tempfile.TemporaryDirectory()
        self.vault = BoxVault(pathlib.Path(self.directory.name) / "box.vault", use_dpapi=False)
        box_private, self.box_public = _pem_pair(BOX_SCALAR)
        self.vault.save({"site_key": "sck_x", "private_key_pem": box_private})
        self.backend = _Backend()
        self.streams = StreamSet()
        self.probed = []

        def probe(host, port, username, password, *, family=None, thumbnail=None):
            self.probed.append((host, port, username, password, family))
            return ProbeResult(family="dahua", channels=[{"channel": 1, "thumbnail": "x"}, {"channel": 2}])

        from syncam_ai.site_worker.setup_agent import SetupAgent

        self.agent = SetupAgent(self.backend, self.vault, self.streams, discover=lambda: [FoundDevice(ip="192.168.1.108", ports={554, 37777}, family="dahua")],
                                probe=probe, thumbnail=None, make_stream=_Stream, run_async=False)

    def tearDown(self):
        self.directory.cleanup()

    def test_scan_probe_adopt(self):
        from syncam_ai.site_worker.setup_agent import vault_camera_sources

        self.agent.handle([{"id": "t1", "type": "scan"}])
        self.assertEqual(self.backend.results["t1"]["result"]["devices"][0]["device_id"], "192.168.1.108:554")
        self.agent.handle([{"id": "t2", "type": "probe", "payload": {"device": {"ip": "192.168.1.108", "rtsp_port": 554, "family": "dahua"}, "credentials": VECTOR_ENVELOPE}}])
        result = self.backend.results["t2"]
        self.assertEqual((result["status"], result["result"]["device_id"], len(result["result"]["channels"])), ("done", "192.168.1.108:554", 2))
        self.assertEqual(self.probed[0], ("192.168.1.108", 554, "syncam", "Pa:ss@w/rd#1", "dahua"))
        camera_id = str(uuid.uuid4())
        self.agent.handle([{"id": "t3", "type": "adopt", "payload": {"device_id": "192.168.1.108:554", "cameras": [{"camera_id": camera_id, "channel": 2}]}}])
        self.assertEqual(self.backend.results["t3"], {"status": "done", "result": {"adopted": [camera_id]}})
        self.assertTrue(self.streams[camera_id].started, "the new camera starts streaming without a restart")
        self.assertIn("channel=2&subtype=1", self.streams[camera_id].source)
        self.assertEqual(list(vault_camera_sources(self.vault)), [camera_id], "adopted cameras come back after a restart")
        self.assertNotIn(b"Pa:ss", self.vault.path.read_bytes(), "the camera password is stored encrypted")
        self.agent.handle([{"id": "t3", "type": "adopt", "payload": {}}])
        self.assertEqual(self.backend.results["t3"]["status"], "done", "a task id runs only once")

    def test_rejects_internet_hosts_wrong_keys_and_bad_input(self):
        for task_id, payload in {
            "a": {"device": {"ip": "8.8.8.8", "rtsp_port": 554}, "credentials": VECTOR_ENVELOPE},
            "b": {"device": {"ip": "127.0.0.1", "rtsp_port": 554}, "credentials": VECTOR_ENVELOPE},
            "c": {"device": {"ip": "192.168.1.9", "rtsp_port": 99999}, "credentials": VECTOR_ENVELOPE},
            "d": {"device": {"ip": "192.168.1.9", "rtsp_port": 554}, "credentials": dict(VECTOR_ENVELOPE, ct="AAAA")},
        }.items():
            self.agent.handle([{"id": task_id, "type": "probe", "payload": payload}])
            self.assertEqual(self.backend.results[task_id]["status"], "failed", task_id)
        self.assertEqual(self.probed, [], "nothing is probed unless the address is private and the password opens")
        self.agent.handle([{"id": "e", "type": "adopt", "payload": {"device_id": "10.0.0.9:554", "cameras": [{"camera_id": CAMERA, "channel": 1}]}}])
        self.assertIn("password first", self.backend.results["e"]["error"])
        self.agent.handle([{"id": "f", "type": "nonsense"}])
        self.assertEqual(self.backend.results["f"]["status"], "failed")

    def test_unpaired_box_cannot_open_credentials(self):
        self.vault.save({"site_key": "sck_x"})
        self.agent.handle([{"id": "p", "type": "probe", "payload": {"device": {"ip": "192.168.1.108"}, "credentials": VECTOR_ENVELOPE}}])
        self.assertIn("not paired", self.backend.results["p"]["error"])


class RuntimePlumbingTest(unittest.TestCase):
    def test_stream_set_adds_once_and_iterates_safely(self):
        streams = StreamSet({"a": _Stream("a", "0")})
        stream = _Stream("b", "1")
        self.assertTrue(streams.add("b", stream))
        self.assertFalse(streams.add("b", _Stream("b", "2")))
        self.assertTrue(stream.started)
        for camera_id in streams:
            streams.add(camera_id + "x", _Stream("c", "3"))  # adding while iterating is safe
        self.assertEqual(len(streams), 4)

    def test_relay_hands_tasks_to_the_agent_even_with_live_view_off(self):
        backend, received = _Backend(), []
        backend.tasks = [{"id": "t1", "type": "scan"}]
        relay = LiveRelay(backend, StreamSet(), on_tasks=received.extend, send_frames=False, clock=lambda: 10.0)
        relay.step()
        self.assertEqual(received, [{"id": "t1", "type": "scan"}])
        self.assertEqual(relay.wanted, frozenset())

    def test_config_without_cameras_is_allowed(self):
        config = parse_local_config({"backend_url": "https://x", "site_key": "k", "models": {"detector": "d.onnx"}}, environ={})
        self.assertEqual(dict(config.cameras), {})


if __name__ == "__main__":
    unittest.main()
