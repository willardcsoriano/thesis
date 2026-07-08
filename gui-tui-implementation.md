# SynapseOS — GUI & TUI Implementation Reference

This document outlines the concrete, technical step-by-step implementation of the SynapseOS user interface modes (TUI and GUI). It serves as an engineering blueprint showing how to build the session takeover without modifying the upstream code of Debian or XFCE.

---

## 1. Shared Architecture

Both modes wrap the same Go backend runtime. The application is packaged as a single, self-contained binary that interacts with:
1.  **Ollama REST API:** Local HTTP requests to `localhost:11434` for model token streaming.
2.  **OS Subprocess Engine:** Go's `os/exec` package for spawning bash commands, capturing `stdout`/`stderr` streams, and managing subprocess lifecycles.
3.  **Safety Logic:** An in-memory confirmation parser and undo stack.

```
                  ┌────────────────────────┐
                  │   SynapseOS Go Core    │
                  └───────────┬────────────┘
                              │
             ┌────────────────┴────────────────┐
             ▼                                 ▼
    [ TUI Mode ]                      [ GUI Mode ]
    Bubbletea/Lipgloss in             Fullscreen Xfwm4
    any standard terminal            takeover (Kiosk Session)
```

---

## 2. TUI (Terminal User Interface) Mode

TUI mode is the default interactive CLI shell for servers, remote SSH connections, and local terminal sessions.

### Technical Stack
*   **Framework:** [bubbletea](https://github.com/charmbracelet/bubbletea), which implements the Elm Architecture (Model-Update-View loop) in Go.
*   **Styling & Layout:** [lipgloss](https://github.com/charmbracelet/lipgloss) for grid layouts, borders, and typography formatting.
*   **Viewport:** Bubbletea's native scrollable viewport component for terminal output.

### Loop Cycle
1.  **Model:** Stores conversation history, current user input, cursor positions, loading/spinner state, and command execution logs.
2.  **Update:** 
    *   Listens for keystrokes.
    *   On `Enter`, it fires a command (an asynchronous `tea.Cmd`) to query Ollama.
    *   Streams tokens from the REST API into the view using a custom goroutine channel.
    *   If a bash command is proposed, it pauses to await confirmation (if flagged by the confirmation gate).
3.  **View:** Renders the text box, scrollable viewport, and status indicators using standard ANSI escape sequences.

---

## 3. GUI Takeover Mode (Thesis Study Prototype)

The GUI mode is designed to act as the primary, fullscreen interface for the user study. It takes over the graphical session without running full desktop panels, desktop icons, or application menus.

### The Takeover Mechanism (No Custom Compositor)
Instead of starting a full desktop session manager (`xfce4-session`) that brings up the panel and desktop icons, we configure the Linux display manager (e.g., LightDM or GDM) to boot a custom, minimal X11 session.

#### 1. Session Registration
We add a custom desktop entry file at `/usr/share/xsessions/synapseos.desktop`:
```ini
[Desktop Entry]
Name=SynapseOS
Comment=Conversational Session Layer for Linux
Exec=/usr/bin/synapseos-session
Type=Application
DesktopNames=SynapseOS
```

#### 2. The Startup Kiosk Script (`/usr/bin/synapseos-session`)
This script executes immediately after login, bypassing the normal XFCE startup sequence:
```bash
#!/bin/bash
# 1. Start the XFCE Window Manager in daemon mode in the background.
# This provides window focusing, window borders, and keybindings without panels.
xfwm4 --daemon &

# 2. Run the SynapseOS app. The 'exec' replaces the shell script process.
# If SynapseOS exits/closes, the X session terminates, logging the user out.
exec /usr/bin/synapseos-gui --fullscreen
```

### Packaging the GUI Application
To display the fullscreen chat interface, you have two lightweight options:

*   **Option A: Fullscreen Terminal Wrapper (Humblest Prototype)**
    Configure a standard, fast terminal emulator (like `kitty` or `xfce4-terminal`) to launch borderless and fullscreen on startup, executing the Go TUI executable directly. Because there are no desktop panels, the user is locked into this fullscreen terminal environment.
*   **Option B: Go WebView wrapper (Fyne / Webview)**
    Wrap your Go program in a lightweight webview or GUI window (using libraries like `github.com/webview/webview` or `fyne.io/fyne`), setting the window properties to fullscreen and disabling window decorations.

---

## 4. Post-Thesis "Overlay Mode" (Product Vision)

Once the user study is complete, SynapseOS can be run as a standard desktop overlay rather than a complete session takeover.

### The Mechanism
1.  **Visibility:** The traditional XFCE desktop environment (panels, files, traditional applications) stays completely visible and usable.
2.  **Hotkey Summon:** We register a global keyboard shortcut (e.g., `Super+Space`) via XFCE's shortcut settings (`xfconf-query`). This hotkey triggers a toggle command:
    ```bash
    synapseos-cli --toggle-window
    ```
3.  **Floating Window:** The SynapseOS GUI program runs as a borderless floating panel that slides in/out of focus, similar to Spotlight on macOS or Alfred, letting the user invoke system tasks while looking at their traditional GUI applications.
