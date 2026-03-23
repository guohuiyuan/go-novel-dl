#![windows_subsystem = "windows"]

use std::env;
use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use tao::event_loop::{ControlFlow, EventLoop};
use tao::window::WindowBuilder;
use wry::{WebContext, WebViewBuilder};

#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;

mod window_config {
    pub const TITLE: &str = "Novel DL Desktop";
    pub const WIDTH: f64 = 1280.0;
    pub const HEIGHT: f64 = 820.0;
}

mod server_config {
    pub const PORT: &str = "37888";
    pub const URL_PATH: &str = "/novel/";
    pub const STARTUP_TIMEOUT_MS: u64 = 15_000;
    pub const STARTUP_POLL_MS: u64 = 250;
    pub const CONNECT_TIMEOUT_MS: u64 = 500;

    #[cfg(target_os = "windows")]
    pub const BINARY_NAME: &str = "novel-dl.exe";
    #[cfg(not(target_os = "windows"))]
    pub const BINARY_NAME: &str = "novel-dl";
}

mod system_config {
    #[cfg(target_os = "windows")]
    pub const CREATE_NO_WINDOW_FLAG: u32 = 0x08000000;
}

#[cfg(target_os = "windows")]
static NOVEL_DL_BINARY: &[u8] = include_bytes!(concat!(env!("CARGO_MANIFEST_DIR"), "/../novel-dl.exe"));
#[cfg(not(target_os = "windows"))]
static NOVEL_DL_BINARY: &[u8] = include_bytes!(concat!(env!("CARGO_MANIFEST_DIR"), "/../novel-dl"));

enum StartupContent {
    ServerUrl(String),
    ErrorHtml(String),
}

fn main() -> wry::Result<()> {
    let app_data_dir = prepare_app_data_dir().expect("failed to prepare app data directory");
    let webview_data_dir = app_data_dir.join("webview");
    fs::create_dir_all(&webview_data_dir).expect("failed to create webview data directory");

    let log_path = app_data_dir.join("logs").join("desktop-backend.log");
    let log_file = open_log_file(&log_path).ok();

    let temp_binary_path = extract_backend_binary();
    let server_url = format!(
        "http://localhost:{}{}",
        server_config::PORT,
        server_config::URL_PATH
    );
    let server_prefix = server_url.clone();

    let mut child = start_backend(&temp_binary_path, &app_data_dir, log_file.as_ref());
    let startup_content = match child.as_mut() {
        Some(process) => match wait_for_server(process) {
            Ok(()) => StartupContent::ServerUrl(server_url.clone()),
            Err(message) => StartupContent::ErrorHtml(build_startup_error_page(
                &message,
                &server_url,
                &app_data_dir,
                Some(&log_path),
                read_log_tail(&log_path, 80),
            )),
        },
        None => StartupContent::ErrorHtml(build_startup_error_page(
            "failed to start the embedded novel-dl backend",
            &server_url,
            &app_data_dir,
            Some(&log_path),
            read_log_tail(&log_path, 80),
        )),
    };

    let event_loop = EventLoop::new();
    let window = WindowBuilder::new()
        .with_title(window_config::TITLE)
        .with_inner_size(tao::dpi::LogicalSize::new(
            window_config::WIDTH,
            window_config::HEIGHT,
        ))
        .build(&event_loop)
        .unwrap();

    let mut web_context = WebContext::new(Some(webview_data_dir.clone()));
    let builder = WebViewBuilder::new(&window)
        .with_web_context(&mut web_context)
        .with_new_window_req_handler(move |url| {
            let _ = open::that(&url);
            false
        })
        .with_navigation_handler(move |nav| {
            let url = nav.as_str();
            if url == "about:blank" || url.starts_with("data:") {
                return true;
            }

            if !url.starts_with(&server_prefix) {
                let _ = open::that(url);
                return false;
            }

            true
        });

    let builder = match startup_content {
        StartupContent::ServerUrl(url) => builder.with_url(&url),
        StartupContent::ErrorHtml(html) => builder.with_html(html),
    };
    let _webview = builder.build()?;

    event_loop.run(move |event, _, control_flow| {
        *control_flow = ControlFlow::Wait;

        if let tao::event::Event::WindowEvent {
            event: tao::event::WindowEvent::CloseRequested,
            ..
        } = event
        {
            window.set_visible(false);

            if let Some(child) = child.as_mut() {
                let _ = child.kill();
                let _ = child.wait();
            }

            for _ in 0..5 {
                if fs::remove_file(&temp_binary_path).is_ok() || !temp_binary_path.exists() {
                    break;
                }
                thread::sleep(Duration::from_millis(50));
            }

            *control_flow = ControlFlow::Exit;
        }
    });
}

fn extract_backend_binary() -> PathBuf {
    let temp_dir = env::temp_dir();
    let unique_name = format!("{}_{}", std::process::id(), server_config::BINARY_NAME);
    let temp_binary_path = temp_dir.join(unique_name);

    if temp_binary_path.exists() {
        let _ = fs::remove_file(&temp_binary_path);
    }

    fs::write(&temp_binary_path, NOVEL_DL_BINARY).expect("failed to write embedded backend binary");

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;

        let mut perms = fs::metadata(&temp_binary_path)
            .expect("failed to read backend metadata")
            .permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&temp_binary_path, perms)
            .expect("failed to make embedded backend executable");
    }

    temp_binary_path
}

fn start_backend(
    backend_path: &Path,
    app_data_dir: &Path,
    log_file: Option<&std::fs::File>,
) -> Option<Child> {
    let mut cmd = Command::new(backend_path);
    cmd.arg("web")
        .arg("--no-browser")
        .arg("-p")
        .arg(server_config::PORT)
        .current_dir(app_data_dir);

    if let Some(file) = log_file {
        if let Ok(stdout) = file.try_clone() {
            cmd.stdout(Stdio::from(stdout));
        }
        if let Ok(stderr) = file.try_clone() {
            cmd.stderr(Stdio::from(stderr));
        }
    }

    #[cfg(target_os = "windows")]
    {
        cmd.creation_flags(system_config::CREATE_NO_WINDOW_FLAG);
    }

    cmd.spawn().ok()
}

fn wait_for_server(child: &mut Child) -> Result<(), String> {
    let timeout = Duration::from_millis(server_config::STARTUP_TIMEOUT_MS);
    let deadline = Instant::now() + timeout;

    loop {
        if is_server_ready() {
            return Ok(());
        }

        match child.try_wait() {
            Ok(Some(status)) => {
                return Err(format!(
                    "the backend exited before the desktop UI was ready: {}",
                    status
                ));
            }
            Ok(None) => {}
            Err(err) => return Err(format!("failed to inspect backend status: {}", err)),
        }

        if Instant::now() >= deadline {
            return Err(format!(
                "timed out after {} seconds while waiting for the backend",
                timeout.as_secs()
            ));
        }

        thread::sleep(Duration::from_millis(server_config::STARTUP_POLL_MS));
    }
}

fn is_server_ready() -> bool {
    let port = match server_config::PORT.parse::<u16>() {
        Ok(port) => port,
        Err(_) => return false,
    };

    let addr = SocketAddr::from(([127, 0, 0, 1], port));
    let mut stream = match TcpStream::connect_timeout(
        &addr,
        Duration::from_millis(server_config::CONNECT_TIMEOUT_MS),
    ) {
        Ok(stream) => stream,
        Err(_) => return false,
    };

    let _ = stream.set_read_timeout(Some(Duration::from_millis(
        server_config::CONNECT_TIMEOUT_MS,
    )));
    let _ = stream.set_write_timeout(Some(Duration::from_millis(
        server_config::CONNECT_TIMEOUT_MS,
    )));

    let request = format!(
        "GET {} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
        server_config::URL_PATH
    );
    if stream.write_all(request.as_bytes()).is_err() {
        return false;
    }

    let mut response = [0_u8; 128];
    let bytes_read = match stream.read(&mut response) {
        Ok(bytes_read) => bytes_read,
        Err(_) => return false,
    };
    if bytes_read == 0 {
        return false;
    }

    let head = String::from_utf8_lossy(&response[..bytes_read]);
    head.contains(" 200 ") || head.contains(" 301 ") || head.contains(" 302 ")
}

fn open_log_file(log_path: &Path) -> io::Result<std::fs::File> {
    if let Some(parent) = log_path.parent() {
        fs::create_dir_all(parent)?;
    }

    let mut file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(log_path)?;
    writeln!(file, "\n== Novel DL Desktop launch ==")?;
    Ok(file)
}

fn build_startup_error_page(
    message: &str,
    server_url: &str,
    app_data_dir: &Path,
    log_path: Option<&Path>,
    log_excerpt: Option<String>,
) -> String {
    let log_line = log_path
        .map(|path| {
            format!(
                "<p><strong>Backend log:</strong> {}</p>",
                html_escape(&path.display().to_string())
            )
        })
        .unwrap_or_default();

    let log_excerpt_block = log_excerpt
        .filter(|content| !content.trim().is_empty())
        .map(|content| {
            format!(
                "<p><strong>Last backend log lines:</strong></p><code>{}</code>",
                html_escape(&content)
            )
        })
        .unwrap_or_default();

    format!(
        r#"<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Novel DL Desktop</title>
  <style>
    body {{
      margin: 0;
      font-family: "Segoe UI", sans-serif;
      background: linear-gradient(180deg, #eff6ff, #dbeafe);
      color: #0f172a;
      display: flex;
      min-height: 100vh;
      align-items: center;
      justify-content: center;
      padding: 24px;
      box-sizing: border-box;
    }}
    .card {{
      width: min(760px, 100%);
      background: rgba(255, 255, 255, 0.94);
      border: 1px solid rgba(148, 163, 184, 0.32);
      border-radius: 22px;
      padding: 28px;
      box-shadow: 0 22px 54px rgba(37, 99, 235, 0.18);
    }}
    h1 {{ margin: 0 0 12px; font-size: 28px; }}
    p {{ margin: 10px 0; line-height: 1.6; }}
    code {{
      display: block;
      margin-top: 8px;
      background: #eff6ff;
      border-radius: 12px;
      padding: 12px;
      overflow-wrap: anywhere;
      white-space: pre-wrap;
    }}
    .actions {{
      display: flex;
      gap: 12px;
      margin-top: 20px;
      flex-wrap: wrap;
    }}
    button {{
      border: 0;
      border-radius: 999px;
      padding: 12px 18px;
      font-size: 15px;
      cursor: pointer;
      background: #1d4ed8;
      color: #fff;
      font-weight: 700;
    }}
  </style>
</head>
<body>
  <div class="card">
    <h1>Desktop backend failed to start</h1>
    <p>{}</p>
    <p><strong>App data directory:</strong> {}</p>
    {}
    {}
    <code>{}</code>
    <div class="actions">
      <button onclick="window.location.href='{}'">Retry</button>
      <button onclick="location.reload()">Reload This Page</button>
    </div>
  </div>
</body>
</html>"#,
        html_escape(message),
        html_escape(&app_data_dir.display().to_string()),
        log_line,
        log_excerpt_block,
        html_escape(server_url),
        html_escape(server_url),
    )
}

fn read_log_tail(log_path: &Path, max_lines: usize) -> Option<String> {
    let raw = fs::read_to_string(log_path).ok()?;
    let mut lines: Vec<&str> = raw.lines().collect();
    if lines.len() > max_lines {
        lines = lines.split_off(lines.len() - max_lines);
    }
    let joined = lines.join("\n").trim().to_string();
    if joined.is_empty() {
        None
    } else {
        Some(joined)
    }
}

fn html_escape(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
}

fn resolve_app_data_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        if let Some(base) = env::var_os("LOCALAPPDATA") {
            return PathBuf::from(base).join("go-novel-dl");
        }
    }

    #[cfg(target_os = "macos")]
    {
        if let Some(home) = home_dir() {
            return home
                .join("Library")
                .join("Application Support")
                .join("go-novel-dl");
        }
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        if let Some(base) = env::var_os("XDG_DATA_HOME") {
            return PathBuf::from(base).join("go-novel-dl");
        }
        if let Some(home) = home_dir() {
            return home.join(".local").join("share").join("go-novel-dl");
        }
    }

    env::temp_dir().join("go-novel-dl")
}

fn prepare_app_data_dir() -> io::Result<PathBuf> {
    let preferred = resolve_app_data_dir();
    if fs::create_dir_all(&preferred).is_ok() {
        return Ok(preferred);
    }

    let fallback = env::temp_dir().join("go-novel-dl");
    fs::create_dir_all(&fallback)?;
    Ok(fallback)
}

#[cfg(any(target_os = "macos", all(unix, not(target_os = "macos"))))]
fn home_dir() -> Option<PathBuf> {
    env::var_os("HOME").map(PathBuf::from)
}
