import os
import sys
import argparse
from http.server import BaseHTTPRequestHandler, HTTPServer


def parse_args():
    parser = argparse.ArgumentParser(
        description="a simple http server that returns the contents of the specified file"
    )
    parser.add_argument("file", help="path to the file to be returned")
    parser.add_argument(
        "-p",
        "--port",
        type=int,
        default=8000,
        help="port for starting the server (default 8000)",
    )
    return parser.parse_args()


args = parse_args()

if not os.path.isfile(args.file):
    print(f"file not found: {args.file}")
    sys.exit(1)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        try:
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()

            with open(args.file, "rb") as f:
                self.wfile.write(f.read())
        except Exception as e:
            self.send_response(500)
            self.end_headers()
            self.wfile.write(str(e).encode())


server = HTTPServer(("0.0.0.0", args.port), Handler)

print(f"started: http://localhost:{args.port}")
print(f"served: {args.file}")

server.serve_forever()
