import concurrent.futures
import copy
import json
import os
from pathlib import Path
import socket
import select
import threading
import subprocess
import sys
import tempfile
import time
from urllib.parse import parse_qs, unquote, urlsplit

kernel = str(Path(sys.argv[1]).resolve())
uri = urlsplit(os.environ["SHOES_HY2_TEST_URI"])
query = parse_qs(uri.query)
pin = query.get("pinSHA256", [""])[0].replace(":", "").lower()
assert len(pin) == 64, "Live test requires a pinned certificate"
password = unquote(uri.username or "")
if uri.password is not None:
    password += ":" + unquote(uri.password)
server = uri.hostname
address = f"[{server}]:{uri.port or 443}" if ":" in server else f"{server}:{uri.port or 443}"
hop = {
    "address": address,
    "transport": "quic",
    "quic_settings": {"verify": False, "server_fingerprints": [pin], "sni_hostname": query.get("sni", [server])[0], "alpn_protocols": ["h3"]},
    "protocol": {"type": "hysteria2", "password": password, "download_mbps": 100},
}
curl = str(Path(os.environ["SystemRoot"]) / "System32" / "curl.exe")


def request(port, download=False):
    url = "https://speed.cloudflare.com/__down?bytes=10000000" if download else "https://www.google.com/generate_204"
    result = subprocess.run([curl, "--proxy", f"socks5h://127.0.0.1:{port}", "--max-time", "20", "-s", "-w", "%{stderr}%{http_code} %{size_download} %{speed_download}", url], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True, timeout=25)
    return result.returncode, result.stderr.strip()


class Relay:
    def __init__(self):
        self.local = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.local.bind(('127.0.0.1', 0))
        family, _, _, _, destination = socket.getaddrinfo(server, uri.port or 443, type=socket.SOCK_DGRAM)[0]
        self.upstream = socket.socket(family, socket.SOCK_DGRAM)
        self.upstream.connect(destination)
        self.enabled = threading.Event()
        self.enabled.set()
        self.closed = threading.Event()
        self.thread = threading.Thread(target=self.run)
        self.thread.start()

    def run(self):
        peer = None
        while not self.closed.is_set():
            ready, _, _ = select.select([self.local, self.upstream], [], [], 0.1)
            for source in ready:
                data, address = source.recvfrom(65536)
                if not self.enabled.is_set():
                    continue
                if source is self.local:
                    peer = address
                    self.upstream.send(data)
                elif peer:
                    self.local.sendto(data, peer)

    def close(self):
        self.closed.set()
        self.thread.join(timeout=2)
        assert not self.thread.is_alive(), 'relay thread leaked'
        self.local.close()
        self.upstream.close()


def probe(client, should_pass, expected_error="", relay=None):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    with tempfile.TemporaryDirectory(prefix="hy2-test-", dir=Path(__file__).parent) as directory:
        config = Path(directory) / "config.json"
        config.write_text(json.dumps([{"global_limits": {"quic_receive_window": 16777216, "quic_stream_window": 8388608, "quic_send_window": 4194304}}, {"address": f"127.0.0.1:{port}", "protocol": {"type": "mixed", "udp_enabled": False}, "rules": [{"masks": "0.0.0.0/0", "action": "allow", "client_chain": client}]}]))
        log = Path(directory) / "runtime.log"
        process = subprocess.Popen([kernel, "--no-reload", "-l", str(log), str(config)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=dict(os.environ, SHOES_HY2_DIAGNOSTICS='1', RUST_LOG='info'))
        try:
            deadline = time.monotonic() + 15
            while True:
                if process.poll() is not None:
                    raise RuntimeError("Native kernel exited during startup")
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Native kernel did not start")
                    time.sleep(0.1)
            result = request(port)
            passed = result[0] == 0 and result[1].startswith("204 ")
            assert passed == should_pass, "Unexpected authentication/certificate result: " + str(result)
            if expected_error:
                assert expected_error in log.read_text(encoding="utf-8"), "Failure did not report the expected rejection reason"
            if should_pass and relay is not None:
                assert log.read_text(encoding='utf-8').count('HY2 authenticated:') == 1
                relay.enabled.clear()
                disconnected = time.monotonic()
                assert request(port)[0] != 0, 'request unexpectedly passed during outage'
                time.sleep(max(0, 35 - (time.monotonic() - disconnected)))
                relay.enabled.set()
                result = request(port)
                assert result[0] == 0 and result[1].startswith('204 '), 'reconnect failed'
                assert log.read_text(encoding='utf-8').count('HY2 authenticated:') == 2, 'session was not rebuilt'
                print('UDP outage, timeout, new authenticated session: PASS', flush=True)
            if should_pass and relay is None:
                with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
                    results = list(executor.map(lambda _: request(port), range(8)))
                assert all(code == 0 and output.startswith("204 ") for code, output in results), "Concurrent native requests failed"
                print("8 concurrent requests: PASS", flush=True)
                code, output = request(port, True)
                assert code == 0 and output.startswith("200 10000000 "), "Download incomplete: " + output
                print("10MB native HY2 download (HTTP bytes Bps):", output, flush=True)
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            assert process.poll() is not None, 'kernel process leaked'
    assert not Path(directory).exists(), 'temporary config/log directory leaked'


probe(hop, True)
print("Correct credentials and pin: PASS", flush=True)
invalid = copy.deepcopy(hop)
invalid["protocol"]["password"] = "intentionally-invalid-test-password"
probe(invalid, False, "authentication rejected")
print("Wrong password rejected: PASS", flush=True)
invalid = copy.deepcopy(hop)
invalid["quic_settings"]["server_fingerprints"] = ["00" * 32]
probe(invalid, False, "unknown server fingerprint")
print("Wrong certificate pin rejected: PASS", flush=True)
relay = Relay()
try:
    relayed = copy.deepcopy(hop)
    relayed['address'] = f'127.0.0.1:{relay.local.getsockname()[1]}'
    probe(relayed, True, relay=relay)
finally:
    relay.close()
print('Processes, relay and temporary files cleaned: PASS', flush=True)
