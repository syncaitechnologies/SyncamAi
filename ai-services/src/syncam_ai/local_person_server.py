"""Run the development-only local HTTPS person-inference bridge.

The process is intentionally opt-in and has no default public listener. The
caller must provide a trusted TLS certificate, the one allowed browser origin,
and a checksum-verified external model pair. It does not persist or log frames.
"""

from __future__ import annotations

import argparse
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import ssl
from typing import Type

from syncam_ai.local_person_inference import (
    LocalPersonDetector,
    decode_local_frame,
    detect_local_people,
    local_person_response,
    validate_local_inference_origin,
)
from syncam_ai.person_detection import OpenVinoPersonDetector


class LocalPersonServerConfig:
    """Explicit development-only server configuration."""

    def __init__(
        self,
        *,
        bind: str,
        port: int,
        allowed_origin: str,
        certificate: Path,
        private_key: Path,
    ) -> None:
        if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
            raise ValueError("local inference port must be within 1 through 65535")
        if not certificate.is_file() or not private_key.is_file():
            raise ValueError("local inference requires caller-provided TLS certificate and private key files")
        self.bind = bind
        self.port = port
        self.allowed_origin = validate_local_inference_origin(allowed_origin)
        self.certificate = certificate
        self.private_key = private_key


def build_local_person_server(
    config: LocalPersonServerConfig, detector: LocalPersonDetector
) -> ThreadingHTTPServer:
    """Build, but do not start, the short-lived TLS server for authorized developer testing."""

    handler = _handler_type(config.allowed_origin, detector)
    server = ThreadingHTTPServer((config.bind, config.port), handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certfile=config.certificate, keyfile=config.private_key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    return server


def _handler_type(allowed_origin: str, detector: LocalPersonDetector) -> Type[BaseHTTPRequestHandler]:
    class LocalPersonHandler(BaseHTTPRequestHandler):
        server_version = "SyncCamLocalDevelopment"
        sys_version = ""

        def log_message(self, _format: str, *_args: object) -> None:
            """Never log URLs, client addresses, frame metadata or request contents."""

        def do_OPTIONS(self) -> None:  # noqa: N802
            if self.headers.get("Origin") != allowed_origin:
                self._respond(HTTPStatus.FORBIDDEN)
                return
            self._respond(HTTPStatus.NO_CONTENT, cors=True)

        def do_POST(self) -> None:  # noqa: N802
            if self.headers.get("Origin") != allowed_origin:
                self._respond(HTTPStatus.FORBIDDEN)
                return
            try:
                length = int(self.headers.get("Content-Length", ""))
                if not 1 <= length <= 640 * 480 * 4:
                    raise ValueError
                frame = decode_local_frame(self.path, self.headers.get("Content-Type"), self.rfile.read(length))
                payload = local_person_response(detect_local_people(detector, frame))
            except (TypeError, ValueError, RuntimeError):
                self._respond(HTTPStatus.BAD_REQUEST, cors=True)
                return
            self._respond(HTTPStatus.OK, payload, cors=True)

        def _respond(
            self,
            status: HTTPStatus,
            payload: dict[str, object] | None = None,
            *,
            cors: bool = False,
        ) -> None:
            encoded = b"" if payload is None else json.dumps(payload, separators=(",", ":")).encode("utf-8")
            self.send_response(status)
            if cors:
                self.send_header("Access-Control-Allow-Origin", allowed_origin)
                self.send_header("Access-Control-Allow-Methods", "OPTIONS, POST")
                self.send_header("Access-Control-Allow-Headers", "Content-Type")
                self.send_header("Vary", "Origin")
            if payload is not None:
                self.send_header("Content-Type", "application/json")
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            if encoded:
                self.wfile.write(encoded)

    return LocalPersonHandler


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the SyncCam development-only local person detector")
    parser.add_argument("--bind", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8443)
    parser.add_argument("--allowed-origin", required=True)
    parser.add_argument("--certificate", type=Path, required=True)
    parser.add_argument("--private-key", type=Path, required=True)
    parser.add_argument("--model-xml", type=Path, required=True)
    parser.add_argument("--model-bin", type=Path, required=True)
    arguments = parser.parse_args()
    config = LocalPersonServerConfig(
        bind=arguments.bind,
        port=arguments.port,
        allowed_origin=arguments.allowed_origin,
        certificate=arguments.certificate,
        private_key=arguments.private_key,
    )
    detector = OpenVinoPersonDetector(xml_path=arguments.model_xml, bin_path=arguments.model_bin)
    with build_local_person_server(config, detector) as server:
        server.serve_forever()


if __name__ == "__main__":
    main()
