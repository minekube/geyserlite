// SPDX-License-Identifier: MIT
use std::process::Stdio;
use std::sync::atomic::Ordering;

use tempfile::TempDir;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::Command;
use tokio::time::{Duration, sleep};
use tracing::{Level, debug, info, warn};

use crate::backoff::Backoff;
use crate::config::{render_config, write_floodgate_key, write_permissions_yml};
use crate::error::{Error, Result};
use crate::locate::locate_binary;
use crate::server::Server;

pub(crate) struct SubprocessRunner;

impl SubprocessRunner {
    pub async fn run(self, srv: &Server) -> Result<()> {
        let binary = locate_binary(&srv.opts)?;
        info!(path = %binary.display(), "located geyserlite binary");

        let workdir = TempDir::new()?;
        write_floodgate_key(workdir.path(), &srv.opts)?;
        render_config(workdir.path(), &srv.opts)?;
        write_permissions_yml(workdir.path())?;

        let policy = srv.opts.restart_policy.expect("validated default");
        let mut backoff = Backoff::new(policy.min_backoff, policy.max_backoff);

        let mut attempt: usize = 0;
        loop {
            attempt += 1;
            if policy.max_retries > 0 && attempt > policy.max_retries {
                return Err(Error::MaxRestarts(policy.max_retries));
            }

            let result = run_once(srv, &binary, workdir.path()).await;
            match result {
                Ok(()) => return Ok(()),
                Err(e) if srv.cancel.is_cancelled() => {
                    debug!(?e, "shutdown signaled");
                    return Ok(());
                }
                Err(e) => {
                    let wait = backoff.next();
                    warn!(?e, ?wait, attempt, "geyser exited; restarting after backoff");
                    tokio::select! {
                        _ = sleep(wait) => {},
                        _ = srv.cancel.cancelled() => return Ok(()),
                    }
                }
            }
        }
    }
}

async fn run_once(srv: &Server, binary: &std::path::Path, workdir: &std::path::Path) -> Result<()> {
    let jvm_args = srv.opts.jvm_args.as_deref().unwrap_or(&[]);
    let mut cmd = Command::new(binary);
    cmd.arg("--nogui")
        .args(jvm_args)
        .current_dir(workdir)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);

    #[cfg(unix)]
    cmd.process_group(0);

    let mut child = cmd.spawn()?;
    let pid = child.id().unwrap_or(0);
    info!(pid, "started geyserlite subprocess");

    let stdout = child.stdout.take().expect("stdout piped");
    let stderr = child.stderr.take().expect("stderr piped");

    let healthy = srv.healthy.clone();
    let stdout_task = tokio::spawn(forward_lines(stdout, Level::INFO, "stdout", Some(healthy.clone())));
    let stderr_task = tokio::spawn(forward_lines(stderr, Level::WARN, "stderr", None));

    let cancel = srv.cancel.clone();
    let exit_status = tokio::select! {
        status = child.wait() => status?,
        _ = cancel.cancelled() => {
            // Graceful: SIGTERM, then SIGKILL on timeout.
            #[cfg(unix)]
            if let Some(id) = child.id() {
                let _ = nix_kill(id, libc_sigterm());
            }
            let timeout = srv.opts.shutdown_timeout.unwrap_or(Duration::from_secs(30));
            tokio::select! {
                status = child.wait() => status?,
                _ = sleep(timeout) => {
                    let _ = child.kill().await;
                    child.wait().await?
                }
            }
        }
    };

    healthy.store(false, Ordering::Relaxed);
    let _ = stdout_task.await;
    let _ = stderr_task.await;

    if exit_status.success() || srv.cancel.is_cancelled() {
        Ok(())
    } else {
        Err(Error::Subprocess(exit_status.code().unwrap_or(-1)))
    }
}

async fn forward_lines<R: tokio::io::AsyncRead + Unpin>(
    reader: R,
    level: Level,
    stream: &'static str,
    healthy: Option<std::sync::Arc<std::sync::atomic::AtomicBool>>,
) {
    let mut lines = BufReader::new(reader).lines();
    while let Ok(Some(line)) = lines.next_line().await {
        match level {
            Level::WARN => warn!(stream, "{}", line),
            Level::ERROR => tracing::error!(stream, "{}", line),
            _ => info!(stream, "{}", line),
        }
        if let Some(flag) = &healthy {
            if !flag.load(Ordering::Relaxed) && line_is_done(&line) {
                flag.store(true, Ordering::Relaxed);
            }
        }
    }
}

/// Detect Geyser's "Done (X.Xs)!" boot completion message, ignoring ANSI color codes.
fn line_is_done(line: &str) -> bool {
    let stripped = strip_ansi(line);
    stripped.contains("Done (")
}

fn strip_ansi(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut chars = s.chars().peekable();
    while let Some(c) = chars.next() {
        if c == '\x1b' && chars.peek() == Some(&'[') {
            chars.next(); // consume '['
            for ch in chars.by_ref() {
                if ch == 'm' {
                    break;
                }
            }
        } else {
            out.push(c);
        }
    }
    out
}

#[cfg(unix)]
fn nix_kill(pid: u32, signal: i32) -> std::io::Result<()> {
    // Avoid pulling in nix crate just for this.
    // SAFETY: kill is signal-safe and doesn't dereference user pointers.
    let rc = unsafe { libc_kill(pid as i32, signal) };
    if rc == 0 { Ok(()) } else { Err(std::io::Error::last_os_error()) }
}

#[cfg(unix)]
unsafe extern "C" {
    #[link_name = "kill"]
    fn libc_kill(pid: i32, sig: i32) -> i32;
}

#[cfg(unix)]
fn libc_sigterm() -> i32 { 15 }

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_done_with_ansi() {
        assert!(line_is_done("\x1b[36;1mINFO\x1b[m Done (1.234s)! Run /geyser help"));
        assert!(line_is_done("[INFO] Done (1.0s)!"));
        assert!(!line_is_done("Loading extensions..."));
    }
}
