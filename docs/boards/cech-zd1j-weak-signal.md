# 検証結果（2026-11-07）

対象: Windows x64 / USB直結 / BonDriver_coax（Sony torne CECH-ZD1 J）

- `cargo test -p bondriver_coax`: 26件成功。
- 模擬サーバーがHTTPヘッダーを返さない場合も、操作・選局キャンセル・終了が応答する。
- 待機中に別チャンネルを指定した際、古い選局の結果が復活しない。
- HTTP接続が開いたままTSデータが途絶えた場合、5秒の無通信検出後に同じチャンネルを開き直し、受信を再開する。
- `cargo build --release -p bondriver_coax`: 成功。
- 最終DLLをWindows AF_UNIXソケット経由で呼び出す実ABIテスト: 成功。
  SetChannel=0ms、状態取得=0ms、再選局受付=0ms、CloseTuner=0ms、Release=1.5163ms（計測分解能内の参考値）。
- clippyはローカルツールチェーンに未導入のため未実施。
- チューナー実機・実電波を弱めた状態での動作確認は未実施。受信感度を改善するものではない。

DLL SHA-256: `dc5ecbde4233ddce2996aca6638d657601c32f453ec06d32915ed6f382512131`

## ソースと再ビルド

BonDriver_coax元リポジトリ: https://github.com/Till0196/BonDriver_coax
基点コミット: `ce82552dc645146402c332d22650eaefdafc147d`
coax-rs固定コミット: `5d3dad3ce60a6223b665e9dac8beed88ebdc7e9e`

このパッチは以前の再接続修正と今回の修正を含む完全差分です。基点コミットのクリーンなソースへ一度だけ適用してください。

```powershell
git clone https://github.com/Till0196/BonDriver_coax.git
cd BonDriver_coax
git checkout ce82552dc645146402c332d22650eaefdafc147d
git apply ../BonDriver_coax-weak-signal.patch
$env:RUSTUP_TOOLCHAIN = 'stable-x86_64-pc-windows-msvc'
cargo test -p bondriver_coax
cargo build --release -p bondriver_coax
```

Windows x64用RustとMSVCのC++ビルド環境が必要です。テストはローカルソケットを使用します。

追加の実ABI検証はリポジトリの `docs/verification/bondriver_weak_signal_abi.go` を参照。
コピーしたテスト用DLLを指定して実行してください（同名INIをテスト設定で上書きします）。
