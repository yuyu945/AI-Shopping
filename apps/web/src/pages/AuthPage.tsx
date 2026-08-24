import { useState } from "react";
import { api as defaultApi, setToken as defaultSetToken } from "../api/client";

type AuthApi = {
  login: (input: { email: string; password: string }) => Promise<{ access_token?: string; token?: string }>;
  register: (input: { email: string; password: string }) => Promise<{ access_token?: string; token?: string }>;
  setToken: (token: string) => void;
};

type AuthPageProps = {
  api?: AuthApi;
};

const authApi: AuthApi = {
  login: defaultApi.login,
  register: defaultApi.register,
  setToken: defaultSetToken,
};

export default function AuthPage({ api = authApi }: AuthPageProps) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState("");

  async function submit() {
    const action = mode === "login" ? api.login : api.register;
    const result = await action({ email, password });
    const token = result.access_token || result.token;
    if (token) {
      api.setToken(token);
      setStatus(mode === "login" ? "已登录。" : "已注册并登录。");
      window.location.hash = "#/products";
    } else {
      setStatus("认证响应缺少 token。");
    }
  }

  return (
    <section className="authPanel">
      <p className="eyebrow">Account</p>
      <h1>{mode === "login" ? "登录" : "注册"}</h1>
      <div className="segmented">
        <button
          aria-label="Use login mode"
          className={mode === "login" ? "selected" : ""}
          onClick={() => setMode("login")}
          type="button"
        >
          Login
        </button>
        <button
          aria-label="Use register mode"
          className={mode === "register" ? "selected" : ""}
          onClick={() => setMode("register")}
          type="button"
        >
          Register
        </button>
      </div>
      <label className="fieldLabel">
        Email
        <input autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} />
      </label>
      <label className="fieldLabel">
        Password
        <input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} />
      </label>
      <button className="primaryButton" disabled={!email || !password} onClick={submit} type="button">
        {mode === "login" ? "Login" : "Register"}
      </button>
      {status && <p className="successText">{status}</p>}
    </section>
  );
}
