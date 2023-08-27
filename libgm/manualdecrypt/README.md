# manualdecrypt
This tool can be used to inspect requests that the messages.google.com/web app sends

0. Install [Go](https://go.dev/dl/) 1.20 or higher and `protoc`
   (`sudo apt install protobuf-compiler` on Debian).
1. Clone this repository (`git clone https://github.com/mautrix/gmessages.git`).
2. Enter this directory (`cd libgm/manualdecrypt`) and compile it (`go build`).
3. Run `./manualdecrypt`
   * On first run, it'll ask for `pr_crypto_msg_enc_key` and `pr_crypto_msg_hmac`
     from the localStorage of the messages for web app.
4. Find the relevant `SendMessage` HTTP request in browser devtools and copy
   the entire raw request body (a bunch of nested JSON arrays).
5. Paste the request body into manualdecrypt, then press Ctrl+D on a blank line
   to tell it to parse the pasted data.
