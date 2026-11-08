import os, socket, time, sys, json
path = "/tmp/sil-proto.sock"
if os.path.exists(path): os.unlink(path)
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.bind(path); s.listen(1)
c, _ = s.accept(); c.settimeout(0.2)
t0 = time.time(); sent = False; buf = b""
while time.time() - t0 < 9:
    try:
        d = c.recv(4096)
        if not d: break
        buf += d
    except socket.timeout: pass
    while b"\n" in buf:
        line, buf = buf.split(b"\n", 1)
        print("FW->SIM", line.decode(), flush=True)
    if not sent and time.time() - t0 > 2:
        for m in ({"k":"in","ch":"fan1_rpm","v":1234},{"k":"in","ch":"temp","v":23.5},{"k":"in","ch":"usr1","v":1}):
            c.sendall((json.dumps(m, separators=(",",":"))+"\n").encode())
        sent = True
