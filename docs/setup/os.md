# OSごとの前提条件（直接USBバックエンド共通）

USBデバイスを直接制御するバックエンド（`t230`、`px3`、`px4`系、`us3pout`、`fsusb2`、`fsusb2i`、`s270`、`cech-zd1j`、`tbs5520`系）を使うために、OSごとに必要な設定をまとめます。
デバイス固有の注意点は各バックエンドの文書にあります。

OSのドライバーを経由するバックエンドはここの対象外です。
`dvb`（[dvb.md](../boards/dvb.md#前提)）、`chardev`（[chardev.md](../boards/chardev.md#前提)）、`bda`（[bda.md](../boards/bda.md#前提条件)）の権限は、それぞれの文書の「前提」の節を見てください。

## Linux

必要なのは、`/dev/bus/usb`の下のデバイスノードへの書き込み権限だけです。
読み取り専用で開いたデバイスノードには、usbfsのioctlを発行できません。
既定は`root:root 0664`なので、rootで実行するか、udevルールを追加します。

ファームウェアの置き場所は[firmware.md](firmware.md)にあります。
ファームウェアをどこに置いても、デバイスノードの権限は別に必要です。

```
# /etc/udev/rules.d/70-coax.rules の例
# 対応デバイスのベンダーIDすべて。GROUPは環境に合わせて（videoかplugdev）
SUBSYSTEM=="usb", ATTR{idVendor}=="0511", MODE="0660", GROUP="video"  # PLEX / DigiBest（px4、pxmlt、isdb2056、s1ur、fsusb2、fsusb2i、us3pout）
SUBSYSTEM=="usb", ATTR{idVendor}=="0572", MODE="0660", GROUP="video"  # DVBSky / MyGica（t230）
SUBSYSTEM=="usb", ATTR{idVendor}=="0b06", MODE="0660", GROUP="video"  # PLEX ASV52xx系（px3）
SUBSYSTEM=="usb", ATTR{idVendor}=="1738", MODE="0660", GROUP="video"  # 同上のコールドID（ASICEN）
SUBSYSTEM=="usb", ATTR{idVendor}=="187f", MODE="0660", GROUP="video"  # Siano（s270）
SUBSYSTEM=="usb", ATTR{idVendor}=="3275", MODE="0660", GROUP="video"  # PLEX PX-S1UD（s270）
SUBSYSTEM=="usb", ATTR{idVendor}=="734c", MODE="0660", GROUP="video"  # TurboSight（tbs系）
```

ルールを書いたら`sudo udevadm control --reload`を実行し、デバイスを挿し直すか`sudo udevadm trigger`を実行します。
実行するユーザーが`video`グループに属していることも確認してください（`id -nG`で確認し、入っていなければ`sudo usermod -aG video $USER`のあと再ログイン）。
ルールを追加すれば、デーモンもCLIも一般ユーザーで動作します（実機で確認済み）。

### カーネルドライバーからのunbind

カーネルのドライバーがUSBインタフェースにバインドしているデバイスは、権限があってもusbfsの`claim`が`EBUSY`になります。
coaxが自動でunbindすることはないので、先にunbindするか、そのドライバーを読み込まない構成にしてください。
ドライバーがバインドしている間、そのデバイスは直接USBバックエンドの一覧に表示されません（`dvb`や`chardev`の側に表示されます）。
`--device`で指定した場合は、バインドしているドライバーを示してエラーを返します。

```sh
# 例: バス1のポート2にあるデバイスをdvb_usb_dvbskyから外す
echo -n '1-2:1.0' | sudo tee /sys/bus/usb/drivers/dvb_usb_dvbsky/unbind
```

| バックエンド | デバイスにバインドすることがあるモジュール |
| --- | --- |
| `t230` | `dvb_usb_dvbsky` |
| `s270` | `smsusb` |
| `px4`、`pxmlt`、`isdb2056`、`s1ur` | `px4_drv` |
| `tbs5520`、`tbs5520se`、`tbs5530` | TBSの`linux_media` |
| `fsusb2`、`fsusb2i`、`us3pout` | メインラインには無し |
| `px3` | 実質存在しません（[px3.md](../boards/px3.md)）|

px4_drvがバインドしているデバイスは、unbindして直接USBバックエンドで使うか、px4_drvのまま`chardev`バックエンドで使うかを選びます（[chardev.md](../boards/chardev.md#px4_drvと直接usbのどちらを使うか)）。

## Windows

対象デバイスに、Windows標準のWinUSBドライバーを割り当てます。
最も簡単なのは、同梱の`.inf`を[`install-winusb.bat`](#コマンドでwinusbを割り当てる)にドラッグする方法です。
[Zadig](https://zadig.akeo.ie/)で`WinUSB`を選んでも同じように使えます。

WinUSBを割り当てている間、そのデバイスはBDAチューナーとしては使えません。
逆に、メーカーのBDAドライバーが割り当てられている間は直接USBバックエンドの一覧に表示されず、`bda`の側にだけ表示されます。
WinUSB以外（libusbKなど）を割り当てたデバイスも同様です。

デバイスは`GUID_DEVINTERFACE_USB_DEVICE`で探索するので、同梱の`.inf`でインストールしたデバイスもZadigでインストールしたデバイスも同じように検出されます。
Zadigはインストールごとに独自のインタフェースGUIDを作りますが、coaxはそのGUIDを使いません。

チューナー名の`bus`番号はcoaxが付けます。
WindowsにはLinuxのようなバス番号が無いので、USBホストコントローラを物理的な位置で並べて1から振ります。
検出の順序に依存しないので、再起動しても名前は変わりません。

Windowsの`--device`は、直結時には`001:002`のようにバス番号とポート番号で指定します。
ハブ配下では`001:002.001`のようにルートからのポート経路を含め、別ハブの同じポート番号と区別します。
USB経路を取得できない場合はデバイスインタフェースパスを使用します。指定値はデバイス一覧の`device`で確認できます。

### 同梱の`.inf`

[`driver/windows/`](../../driver/windows)にバックエンドごとの`.inf`を置いています。
1つの`.inf`にそのバックエンドの対応デバイスがすべて含まれているので、使うバックエンドの`.inf`を1つインストールすれば十分です。

| `.inf` | バックエンド | 対象 |
| --- | --- | --- |
| `px4-winusb.inf` | `px4` | PX-W3U4、PX-Q3U4、PX-W3PE4、PX-Q3PE4、PX-W3PE5、PX-Q3PE5 |
| `pxmlt-winusb.inf` | `pxmlt` | PX-MLT5U、PX-MLT5PE、PX-MLT8PE、DigiBest ISDB6014 V2.0、e-Better DTV02A-5TS-P |
| `isdb2056-winusb.inf` | `isdb2056` | DigiBest ISDB2056、ISDB2056N、PX-M1UR |
| `s1ur-winusb.inf` | `s1ur` | PX-S1UR、DigiBest ISDBT2071（DTV03A-1TU）|
| `px3-winusb.inf` | `px3` | PX-S3U、PX-S3U2、PX-W3U2、PX-W3U3、PX-W3U3 V2 |
| `t230-winusb.inf` | `t230` | MyGica T230、T230C、T230C Lite、T230A、T230C2、T230C2 Lite |
| `s270-winusb.inf` | `s270` | MyGica S270、PLEX PX-S1UD |
| `cech-zd1j-winusb.inf` | `cech-zd1j` | Sony CECH-ZD1 J |
| `fsusb2-winusb.inf` | `fsusb2` | KTV-FSUSB2（初期型）|
| `fsusb2i-winusb.inf` | `fsusb2i` | KTV-FSUSB2/V3（K1212以降）、KTV-FSMINI |
| `us3pout-winusb.inf` | `us3pout` | DigiBest US-3POUT |
| `tbs-winusb.inf` | `tbs5520`、`tbs5520se`、`tbs5530` | TBS 5520、TBS 5520SE、TBS 5530 |

PX-Q3U4、PX-Q3PE4、PX-Q3PE5、PX-MLT8PEは2つのUSBデバイスとして見えますが、`.inf`には両方の識別子が入っています。

`px3`の`.inf`だけは、製品の識別子に加えてコールドID（`1738:5211`、`1738:5216`）も入っています。
ASV52xxのデバイスは、接続した直後はブリッジのブートROMがASICENの識別子で動いていて、ファームウェアもUSB越しに書き込むからです（[px3.md](../boards/px3.md)）。
コールドIDにWinUSBが割り当てられていないと、ファームウェアを書き込む前の段階でデバイスを開けません。

### コマンドでWinUSBを割り当てる

同梱の[`driver/windows/install-winusb.bat`](../../driver/windows/install-winusb.bat)に`.inf`をドラッグします。
batは自己署名の証明書を作って信頼する証明書に登録し、`.inf`に合ったカタログを作って署名し、`pnputil`でインストールします。
複数の`.inf`をまとめてドラッグしても構いません。
ドラッグせずにダブルクリックすると、同じフォルダにある`*-winusb.inf`をすべてインストールします。
batが管理者権限を要求するので、右クリックの「管理者として実行」は不要です。
UACの確認で「はい」を選ぶと、別のウィンドウが開いて処理が進みます。

証明書は`CN=coax WinUSB signing`の1枚を使い回すので、2つ目以降の`.inf`をインストールしても増えません。
2回目以降は`Created certificate`ではなく`Reusing certificate`と表示されます。

WDKもテスト署名も不要です。
Zadigが内部で行っている処理と同じです。

アンインストールするときは、`.inf`を[`driver/windows/uninstall-winusb.bat`](../../driver/windows/uninstall-winusb.bat)にドラッグします。
ドラッグせずにダブルクリックすると、同梱の`.inf`で入れたパッケージを`pnputil`のドライバー一覧から探して`/delete-driver ... /uninstall`し、1つも残っていなければ証明書も証明書ストアから削除します。

#### batの処理内容

中身はPowerShellです。
手動で実行する場合は次のとおりです。

```powershell
$dir = "C:\coax\driver"   # .inf を置いたディレクトリ
$cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=coax WinUSB signing" `
    -CertStoreLocation Cert:\LocalMachine\My -NotAfter (Get-Date).AddYears(2)
Export-Certificate -Cert $cert -FilePath "$dir\coax-signing.cer" | Out-Null
Import-Certificate -FilePath "$dir\coax-signing.cer" -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath "$dir\coax-signing.cer" -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null

New-FileCatalog -Path $dir -CatalogFilePath "$dir\t230-winusb.cat" -CatalogVersion 2 | Out-Null
Set-AuthenticodeSignature -FilePath "$dir\t230-winusb.cat" -Certificate $cert

pnputil /add-driver "$dir\t230-winusb.inf" /install
```

`.cat`の名前は、`.inf`の`CatalogFile`と同じでなければなりません。
上は`t230`の例なので、別のデバイスなら`.cat`の名前をその`.inf`に合わせて同じ手順を実行します。
`New-FileCatalog`はフォルダ全体のハッシュを取るので、batは`.inf`を1つずつ一時フォルダに置いて、その`.inf`だけのカタログを作ります。

アンインストールは逆の順番で行います。
インストールしたドライバーパッケージは、Windowsが`oemN.inf`という名前に変更して保管するので、まず元の名前から`oemN.inf`を探します。

```powershell
pnputil /enum-drivers   # 元の名前が t230-winusb.inf の項の「公開名」を見る
pnputil /delete-driver oem42.inf /uninstall

Get-ChildItem Cert:\LocalMachine\My, Cert:\LocalMachine\Root, Cert:\LocalMachine\TrustedPublisher |
    Where-Object { $_.Subject -eq "CN=coax WinUSB signing" } | Remove-Item
```

`/uninstall`を付けると、ドライバーストアから削除するだけでなく、そのドライバーが割り当てられているデバイスからも外します。
証明書は他の`.inf`のパッケージの検証にも使っているので、削除するのは同梱の`.inf`でインストールしたパッケージがすべて無くなってからです。
batもこの順番で動き、`pnputil /enum-drivers`の出力で`oemN.inf`と元の名前が同じ項に並んでいることを使って対応を取ります。

### デーモンの終了

`coaxd`は通知領域にアイコンを出します。
アイコンを右クリックして「Stop the tuner daemon」を選ぶのが安全な終了方法です。
接続中のクライアントの数を表示し、終了してよいかを尋ねます。
ウィンドウを閉じる、ログオフ、シャットダウン、`Ctrl+C`でも同じように安全に終了します。

デスクトップの無いセッション（サービス、タスクスケジューラ、ssh）には通知領域が無いので、そのことを一度だけ表示し、アイコン無しで動作します。

## macOS

追加の設定は不要です。

## 共通

他のプロセスが対象のデバイスを使っているときは、そのプロセスを終了してから`coaxd`を起動してください。

`coaxd`は二重起動を防ぐため、ハードウェアにアクセスする前にロックを取得します。
ロックの実体はOSによって異なります。

| OS | 実体 | 置き場所 |
| --- | --- | --- |
| Windows | 名前付きミューテックス | 利用者のSIDで囲ったプライベート名前空間 |
| Linux、macOS | `flock`による排他ロック | HTTPソケットの隣、`http.sock.lock` |

どちらもプロセスが終了するとカーネルが解放するので、クラッシュしても`kill -9`で止めても、ロックが残ることはありません。

Windowsはファイルを作りません。
プライベート名前空間は、利用者自身のSIDを持つプロセスしか作ることも開くこともできないので、他のユーザーが同じ名前を先に作って起動を邪魔することはできません。
セッションにも依存しないので、リモートのシェルから起動した`coaxd`とテレビのソフトから起動した`coaxd`も、同じソケットなら互いを検出します。

LinuxとmacOSのロックファイルは、`coaxd`が終了するときに削除します。
ロックを保持したままファイルを削除してから解放し、ロックを取得した側は、パスがまだ自分の開いたファイルを指しているかを確認します。
そのため、削除されたファイルのロックを待っていた別の`coaxd`と二重に起動することはありません。
クラッシュしたときだけファイルが残りますが、動作に影響は無く、次の`coaxd`がそのまま使います。
置き場所はOSが片付けるディレクトリ（`/run`、`$XDG_RUNTIME_DIR`、`$TMPDIR`）です。

LinuxとmacOSのロックは、ソケットのフォルダがあるファイルシステムが`flock`を正しく扱えることが前提です。
NFS、SMB/CIFS、ロックを実装していないFUSE、一部のコンテナのオーバーレイ構成では、ロックが取れたように見えても2つ目の起動を防げないことがあります。
既定の置き場所（`/run`、`$XDG_RUNTIME_DIR`、`$TMPDIR`）はどれもローカルなので、通常は問題になりません。
`--socket-dir`や`COAXD_SOCKET_DIR`を使うときはローカルのディレクトリを指定してください。
Windowsのロックはカーネルオブジェクトなので、ファイルシステムに依存しません。
