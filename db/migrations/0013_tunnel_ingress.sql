-- 0013: tunnels.kind 增加 ingress（C2 入站回调中继）
ALTER TABLE tunnels DROP CONSTRAINT IF EXISTS tunnels_kind_check;
ALTER TABLE tunnels ADD CONSTRAINT tunnels_kind_check CHECK (kind IN ('socks','portfwd','ingress'));
