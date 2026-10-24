# Notice: code from bitchat-android

- Upstream: bitchat-android, https://github.com/permissionlesstech/bitchat-android
- Commit: `dfc67dad41691128bc6e7425bd69f2f409cb5bbf`
- Modified by the MAMÁ team, 2026-10.

This module contains code copied and modified from bitchat-android, licensed under the GNU General Public License v3.0. The MAMÁ repository as a whole is distributed under GPL-3.0; see /LICENSE.

Upstream's README says "public domain", while its `LICENSE.md` is the GPL-3.0 text. We follow GPL-3.0.

## Provenance rule

Every copied file starts with a provenance header naming the upstream commit and path. Adapted files list their changes in that header.

## Kinds of file

| Kind | Files | Changes |
| --- | --- | --- |
| Verbatim | All files copied unchanged except the package rename (`com.bitchat.android` to `com.team27.mama.mesh`) | Package rename only |
| Adapted | `SecurityManager`, `MessageHandler` | Trimmed to what MAMÁ needs; changes listed in each file header |
| Derived | `EncryptionService`, `MeshKeyStore`, `BleMeshNode`, `MeshPacketFactory` | Rewritten from upstream logic; header names the upstream files they derive from |
| Stub | `ui/debug/DebugSettingsManager` (also stands in for `DebugPreferenceManager`), `services/AppStateStore` | Written by MAMÁ with the API the copied code calls, in place of upstream UI and state code that was not copied; header names the upstream files they replace |

## Removed upstream test

`protocol/BinaryProtocolTest.kt` is copied without the test `new sender refuses legacy 11 MiB public file transfer before transmission`, which needs the cut `BitchatFilePacket`. The file header records the removal.
