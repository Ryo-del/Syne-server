import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { open } from "@tauri-apps/plugin-dialog";
import { enable, disable } from "@tauri-apps/plugin-autostart";
import "./SetupWindow.css";

type Props = { onDone: () => void };

export default function SetupWindow({ onDone }: Props) {
  const [name, setName] = useState("");
  const [httpPort, setHttpPort] = useState("8080");
  const [p2pPort, setP2pPort] = useState("4001");
  const [filesPath, setFilesPath] = useState("");
  const [autostart, setAutostart] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const pickFolder = async () => {
    const dir = await open({ directory: true, multiple: false });
    if (typeof dir === "string") setFilesPath(dir);
  };

  const validate = (): string | null => {
    if (!name.trim()) return "Введите название сервера";
    const h = Number(httpPort), p = Number(p2pPort);
    for (const [label, v] of [["HTTP", h], ["P2P", p]] as const) {
      if (!Number.isInteger(v) || v < 1 || v > 65535)
        return `${label} порт должен быть от 1 до 65535`;
    }
    if (h === p) return "Порты должны отличаться";
    if (!filesPath.trim()) return "Выберите папку для файлов";
    return null;
  };

  const submit = async () => {
    const err = validate();
    if (err) return setError(err);
    setError("");
    setBusy(true);
    try {
      await invoke("save_config", {
        config: {
          name: name.trim(),
          http_port: Number(httpPort),
          p2p_port: Number(p2pPort),
          files_path: filesPath,
          autostart,
        },
      });
      autostart ? await enable() : await disable();
      onDone();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="setup-wrap">
      <div className="setup-card">
        <h1>Первичная настройка Syne Server</h1>

        <label>Название
          <input value={name} onChange={(e) => setName(e.target.value)}
                 placeholder="Например: Школа №5" />
        </label>

        <div className="row">
          <label>HTTP порт
            <input value={httpPort} inputMode="numeric"
                   onChange={(e) => setHttpPort(e.target.value.replace(/\D/g, ""))} />
          </label>
          <label>P2P порт
            <input value={p2pPort} inputMode="numeric"
                   onChange={(e) => setP2pPort(e.target.value.replace(/\D/g, ""))} />
          </label>
        </div>

        <label>Путь для раздела «Файлы»
          <div className="path-row">
            <input value={filesPath} onChange={(e) => setFilesPath(e.target.value)}
                   placeholder="/srv/syne/files" />
            <button type="button" className="icon-btn" onClick={pickFolder}
                    title="Выбрать папку">📁</button>
          </div>
        </label>

        <label className="check">
          <input type="checkbox" checked={autostart}
                 onChange={(e) => setAutostart(e.target.checked)} />
          Запускать вместе с устройством
        </label>

        {error && <div className="error">{error}</div>}

        <button className="primary" onClick={submit} disabled={busy}>
          {busy ? "Создание…" : "Создать сервер"}
        </button>
      </div>
    </div>
  );
}