import argparse
import concurrent.futures
import copy
import ctypes
from ctypes import wintypes
import hashlib
import json
import os
from pathlib import Path
import shlex
import socket
import statistics
import subprocess
import tempfile
import time
import ssl

REMOTE = r'''
import hashlib, http.server, json, threading, sys
class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *args): pass
    def do_POST(self):
        remaining = int(self.headers['Content-Length'])
        count = 0
        digest = hashlib.sha256()
        while remaining:
            data = self.rfile.read(min(65536, remaining))
            if not data: return
            count += len(data)
            remaining -= len(data)
            digest.update(data)
        body = json.dumps({'received': count, 'sha256': digest.hexdigest()}).encode()
        self.send_response(200)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()
    def do_GET(self):
        count = int(self.path.strip('/'))
        self.send_response(200)
        self.send_header('Content-Length', str(count))
        self.end_headers()
        while count:
            chunk = b'x' * min(count, 65536)
            self.wfile.write(chunk)
            count -= len(chunk)
        self.wfile.flush()
server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
print(server.server_port, flush=True)
threading.Thread(target=server.serve_forever, daemon=True).start()
sys.stdin.read()
server.shutdown()
server.server_close()
'''


def wait_port(process, port):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError('test process exited')
        try:
            with socket.create_connection(('127.0.0.1', port), timeout=0.2):
                return
        except OSError:
            time.sleep(0.05)
    raise TimeoutError('test listener startup')


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def cpu_seconds(process):
    values = [wintypes.FILETIME() for _ in range(4)]
    success = ctypes.windll.kernel32.GetProcessTimes(wintypes.HANDLE(int(process._handle)), *(ctypes.byref(value) for value in values))
    assert success, 'GetProcessTimes failed'
    return sum((value.dwHighDateTime << 32) + value.dwLowDateTime for value in values[2:]) / 10000000


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('kernel')
    parser.add_argument('--reference', default='D:/v2rayN/bin/sing_box/sing-box.exe')
    parser.add_argument('--config', default='D:/v2rayN/binConfigs/config.json')
    parser.add_argument('--ssh', default='gc-jp')
    parser.add_argument('--rounds', type=int, default=3)
    parser.add_argument('--bytes', type=int, default=4000000)
    parser.add_argument('--parallel', type=int, default=1)
    parser.add_argument('--native-only', action='store_true')
    parser.add_argument('--send-window', type=int, default=4194304)
    parser.add_argument('--min-upload-ratio', type=float, default=0)
    parser.add_argument('--reference-up-mbps', type=int)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    config = json.loads(Path(args.config).read_text(encoding='utf-8-sig'))
    outbound = next(copy.deepcopy(item) for item in config['outbounds'] if item['type'] == 'hysteria2')
    cert = subprocess.check_output(['ssh', args.ssh, 'cat /etc/hysteria/server.crt'], text=True)
    pin = hashlib.sha256(ssl.PEM_cert_to_DER_cert(cert)).hexdigest()
    outbound['tls']['insecure'] = False
    outbound['tls']['certificate'] = cert
    outbound.pop('tag', None)
    if args.reference_up_mbps is not None:
        outbound['up_mbps'] = args.reference_up_mbps
    hop = {'address': f"{outbound['server']}:{outbound['server_port']}", 'transport': 'quic',
           'quic_settings': {'verify': False, 'server_fingerprints': [pin], 'sni_hostname': outbound['tls']['server_name'], 'alpn_protocols': ['h3']},
           'protocol': {'type': 'hysteria2', 'password': outbound['password'], 'download_mbps': outbound.get('down_mbps', 100)}}
    remote = subprocess.Popen(['ssh', args.ssh, 'python3 -u -c ' + shlex.quote(REMOTE)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    rows = []
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text('[]\n')
    try:
        remote_port = int(remote.stdout.readline())
        with tempfile.TemporaryDirectory(prefix='hy2-bench-', dir=Path(__file__).parent) as directory:
            directory = Path(directory)
            payload = directory / 'payload'
            payload.write_bytes(b'x' * args.bytes)
            digest = hashlib.sha256(payload.read_bytes()).hexdigest()
            curl = str(Path(os.environ['SystemRoot']) / 'System32/curl.exe')
            for round_no in range(1, args.rounds + 1):
                order = ['native'] if args.native_only else (['native', 'reference'] if round_no % 2 else ['reference', 'native'])
                for name in order:
                    with socket.socket() as listener:
                        listener.bind(('127.0.0.1', 0))
                        port = listener.getsockname()[1]
                    if name == 'native':
                        (directory / 'native.log').unlink(missing_ok=True)
                        settings = [{'global_limits': {'quic_receive_window': 16777216, 'quic_stream_window': 8388608, 'quic_send_window': args.send_window}}, {'address': f'127.0.0.1:{port}', 'protocol': {'type': 'mixed', 'udp_enabled': False}, 'rules': [{'masks': '0.0.0.0/0', 'action': 'allow', 'client_chain': hop}]}]
                        command = [str(Path(args.kernel).resolve()), '--no-reload', '-l', str(directory / 'native.log')]
                    else:
                        settings = {'log': {'level': 'error'}, 'inbounds': [{'type': 'mixed', 'listen': '127.0.0.1', 'listen_port': port}], 'outbounds': [outbound]}
                        command = [args.reference, 'run', '-c']
                    config_path = directory / 'config.json'
                    config_path.write_text(json.dumps(settings))
                    log = open(directory / 'log', 'w')
                    environment = dict(os.environ, RUST_LOG='error,shoes::hysteria2_client=info', SHOES_HY2_DIAGNOSTICS='1')
                    process = subprocess.Popen(command + [str(config_path)], stdout=log, stderr=log, env=environment)
                    try:
                        wait_port(process, port)
                        expected_stats = 0
                        for direction in ['upload', 'download']:
                            def transfer(index):
                                response = directory / f'response-{index}'
                                command = [curl, '--noproxy', '', '--proxy', f'socks5h://127.0.0.1:{port}', '--max-time', '180', '--silent', '--show-error', '--output', str(response), '--write-out', '%{http_code} %{size_download} %{size_upload}', '-H', 'Expect:', f'http://127.0.0.1:{remote_port}/{args.bytes}']
                                if direction == 'upload':
                                    command += ['--data-binary', '@' + str(payload)]
                                result = subprocess.run(command, capture_output=True, text=True, timeout=190)
                                assert result.returncode == 0, f'{name} {direction} curl failed: {result.returncode}'
                                status, downloaded, uploaded = result.stdout.split()
                                assert status == '200', 'incomplete HTTP response'
                                if direction == 'upload':
                                    body = json.loads(response.read_text())
                                    assert body == {'received': args.bytes, 'sha256': digest}, 'server count/hash mismatch'
                                    assert int(uploaded) == args.bytes
                                else:
                                    assert int(downloaded) == args.bytes
                                    assert response.stat().st_size == args.bytes
                                    assert hashlib.sha256(response.read_bytes()).hexdigest() == digest
                            cpu_started = cpu_seconds(process)
                            started = time.monotonic()
                            with concurrent.futures.ThreadPoolExecutor(max_workers=args.parallel) as executor:
                                list(executor.map(transfer, range(args.parallel)))
                            elapsed = time.monotonic() - started
                            row = {'round': round_no, 'client': name, 'direction': direction, 'bytes_each': args.bytes, 'parallel': args.parallel, 'seconds': round(elapsed, 3), 'mbps': round(args.bytes * args.parallel * 8 / elapsed / 1000000, 3), 'verified': True}
                            row['cpu_core_percent'] = round((cpu_seconds(process) - cpu_started) / elapsed * 100, 2)
                            diagnostic = directory / 'native.log'
                            if name == 'native' and diagnostic.exists():
                                expected_stats += args.parallel
                                deadline = time.monotonic() + 2
                                while True:
                                    entries = [line.split('HY2 stream closed: ', 1)[1] for line in diagnostic.read_text(encoding='utf-8').splitlines() if 'HY2 stream closed: ' in line]
                                    if len(entries) >= expected_stats or time.monotonic() >= deadline:
                                        break
                                    time.sleep(0.05)
                                row['quic_stats'] = entries[expected_stats - args.parallel:expected_stats]
                            rows.append(row)
                            output.write_text(json.dumps(rows, indent=2) + '\n')
                            print(json.dumps({key: value for key, value in row.items() if key != 'quic_stats'}), flush=True)
                    finally:
                        stop(process)
                        log.close()
    finally:
        if remote.stdin:
            remote.stdin.close()
        try:
            remote.wait(timeout=10)
        except subprocess.TimeoutExpired:
            stop(remote)
    for name in ['native', 'reference']:
        for direction in ['upload', 'download']:
            values = [row['mbps'] for row in rows if row['client'] == name and row['direction'] == direction]
            if values:
                print(name, direction, 'median Mbps', statistics.median(values), flush=True)
    assert not directory.exists(), 'temporary directory leaked'
    assert remote.poll() == 0, 'remote endpoint did not shut down cleanly'
    if args.min_upload_ratio:
        medians = {name: statistics.median(row['mbps'] for row in rows if row['client'] == name and row['direction'] == 'upload') for name in ['native', 'reference']}
        assert medians['native'] / medians['reference'] >= args.min_upload_ratio, 'native upload regression against alternating reference'


if __name__ == '__main__':
    main()
