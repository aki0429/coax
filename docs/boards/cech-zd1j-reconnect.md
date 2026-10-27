# TVTestで受信サービス停止後に復帰できない問題

選局の安定化とは別に、受信サービス `coaxd` が終了したあとにTVTestだけが残り、その後の選局も失敗し続けるケースを確認しました。受信サービスが最初に終了した原因そのものは特定できていません。

BonDriver_coaxは `OpenTuner` のときにサービスの起動を確認しますが、従来は後の選局時に再起動を確認しませんでした。また、終了したストリームを受け取り直す処理もありませんでした。

## BonDriverの追加修正

- 各選局時にも `ensure_running` で受信サービスの生存と起動を確認。
- 選局時にチューナー一覧を再取得し、サービス再起動・USB再接続後の古い識別情報を引きずらないよう変更。
- 再生中のストリームが終了してバッファーも空になった場合、最後に受信できたチャンネルを再取得。
- 復帰に失敗した場合は2秒以上間隔を空け、繰り返し呼び出すホストでもビジーループを避ける。
- TVTestからチューナーを閉じたときは、自動復帰も取り消す。

自動復帰はローカルサービスの自動起動を有効にしている構成が対象です。意図的に `StartDaemon` を無効にしている構成では勝手にサービスを起動しません。

## 実機・テスト

Windows x86-64のTVTestとCECH-ZD1 Jで、NHK総合大阪の再生中に当該 `coaxd` プロセスを意図的に停止しました。同じTVTestプロセスを維持したまま、サービスの自動再起動・同じチャンネルへの再選局・映像の復帰を確認しました。

BonDriverの単体テストは23件成功しました。追加テストでは、終了したストリームから同じドライバーインスタンスで復帰すること、およびチューナーを閉じた後は復帰しないことを確認しています。

## 配布と再現方法

リリースの `BonDriver_coax_reconnect_Windows_amd64.zip` は、この追加修正を含む64ビットWindows用のBonDriverです。TVTestを終了してから、実際に読み込んでいる `BonDriver_coax.dll` を置き換えてください。既存の `winscard.dll`・鍵・チャンネル設定を置き換える必要はありません。

このDLLは [Till0196/BonDriver_coax](https://github.com/Till0196/BonDriver_coax) のコミット `ce82552dc645146402c332d22650eaefdafc147d` を元にしています。ソース差分は [BonDriver_coax-reconnect.patch](../../patches/BonDriver_coax-reconnect.patch) です。依存先 `coax-rs` のコミットも `Cargo.lock` の差分に含めています。

```powershell
git clone https://github.com/Till0196/BonDriver_coax.git
cd BonDriver_coax
git checkout ce82552dc645146402c332d22650eaefdafc147d
git apply /path/to/BonDriver_coax-reconnect.patch
cargo test --locked -p bondriver_coax --target x86_64-pc-windows-msvc
cargo build --locked --release -p bondriver_coax --target x86_64-pc-windows-msvc
```

Rust stableとVisual StudioのC++ビルドツールが必要です。今回のビルドにはRust 1.99.1を使用しました。MSVCターゲットだけを使う場合は `RUSTUP_TOOLCHAIN=stable-x86_64-pc-windows-msvc` を設定すると、上流のツールチェーン指定によるクロスビルド用ターゲットの追加を避けられます。

32ビット・ARM64のBonDriverバイナリはこの追加配布物には含みません。受信サービス本体のcoaxの配布対象とは別です。自動復帰の実機確認は短時間の停止試験であり、長時間録画の無停止性を保証するものではありません。
