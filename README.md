# sway-layout-sync

A lightweight, sub-millisecond keyboard layout synchronizer between a host machine and a QEMU/KVM virtual machine running **Sway (Wayland)**.

## Why?

When using Sway on both the host and guest inside QEMU/KVM (`virt-manager` / SPICE), binding keyboard layout switching to **Caps Lock** (`grp:caps_toggle`) triggers a notorious virtualization bug:
* SPICE client tries to synchronize keyboard LED modifiers (`sync-modifiers`) between the host and the guest.
* Because both systems catch the Caps Lock press simultaneously, SPICE detects a state desynchronization and injects synthetic keypresses into the guest after a ~1s timeout.
* This causes the guest's layout to toggle back to the previous language automatically or desync completely.

`sway-layout-sync` solves this by connecting directly to Sway's native IPC binary sockets on both machines. Layout changes on the host are streamed over a local TCP socket to the guest in **< 0.5 ms**, eliminating SPICE modifier conflicts entirely.

## Requirements

1. **Identical layout configurations**:
   The host and guest must have the **exact same layouts in the exact same order** in their Sway configurations (e.g., `xkb_layout "us,ru"` on both).
2. **Change guest Caps Lock behaviour**:
   Replace `grp:caps_toggle` with `caps:shift_caps_cancel` in the guest’s Sway configuration so the guest does not fight with the host. The host alone will handle physical Caps Lock presses.

## Installation

### Option 1: Nix Flakes

1. Add the following to your system `flake.nix` inputs:
```nix
sway-layout-sync = {
  url = "github:fosiodev/sway-layout-sync";
  inputs.nixpkgs.follows = "nixpkgs";
};
```
2. Add `sway-layout-sync` to your `environment.systemPackages`:
```nix
environment.systemPackages = [
  inputs.sway-layout-sync.packages.${pkgs.stdenv.hostPlatform.system}.default
];
```

### Option 2: Manual Build

Requirements: `Go 1.20+` (uses standard library only, zero external dependencies).

```bash
git clone https://github.com/yourusername/sway-layout-sync.git
cd sway-layout-sync
go build -o sway-layout-sync main.go
```

## Sway Configuration (`~/.config/sway/config`)

Set the appropriate keyboard options on each system:

### Host
```sway
input "type:keyboard" {
    xkb_layout "us,ru"
    xkb_options "grp:caps_toggle"
}
```

### Guest
Prevents lone Caps Lock from turning on CAPITAL letters in the VM, while still allowing Shift + Caps Lock to toggle real Caps Lock:
```sway
input "type:keyboard" {
    xkb_layout "us,ru"
    xkb_options "caps:shift_caps_cancel"
}
```

## Usage

1. **Start on Host (Server mode):**
   ```bash
   sway-layout-sync -s 192.168.122.1:8889
   ```

2. **Start on Guest (Client mode):**
   ```bash
   sway-layout-sync 192.168.122.1:8889
   ```

> **Note for NixOS / Linux firewall:** Make sure TCP port `8889` is open on the host's virtual bridge interface (`virbr0`), e.g.:
> ```nix
> networking.firewall.interfaces."virbr0".allowedTCPPorts = [ 8889 ];
> ```

## Disclaimer

This software is provided "as is", without warranty of any kind, express or implied. It communicates over unencrypted TCP sockets and is strictly intended for use within isolated, local virtual networks (such as QEMU's `192.168.122.0/24` bridge) between a host and its guest VM.
