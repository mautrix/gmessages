# v0.2.4 (unreleased)

* Fixed panic handling read receipts if the user isn't connected.
* Fixed some error states being persisted and not being cleared properly
  if the user logs out and back in.

# v0.2.3 (2023-12-16)

* Added error notice if user switches to google account pairing.

# v0.2.2 (2023-11-16)

No notable changes.

# v0.2.1 (2023-10-16)

* Added notice messages to management room if phone stops responding.
* Fixed all Matrix event handling getting blocked by read receipts in some cases.
* Fixed panic if editing Matrix message fails.

# v0.2.0 (2023-09-16)

* Added support for double puppeting with arbitrary `as_token`s.
  See [docs](https://docs.mau.fi/bridges/general/double-puppeting.html#appservice-method-new) for more info.
* Switched to "tablet mode", to allow using the bridge in parallel with
  Messages for Web.
  * You can have two tablets and one web session simultaneously. The bridge
    will now take one tablet slot by default, but you can change the device
    type in the bridge config.
  * Existing users will have to log out and re-pair the bridge to switch to
    tablet mode.
* Added bridging for user avatars from phone.
* Fixed sending messages not working for some users with dual SIMs.
* Fixed message send error status codes from phone not being handled as errors.
* Fixed incoming message and conversation data sometimes going into the wrong
  portals.
* Fixed bridge sometimes getting immediately logged out after pairing.
* Fixed some cases of attachments getting stuck in the "Waiting for file" state.
* Fixed reactions not being saved to the database.
* Fixed various panics.
* Fixed race conditions when handling messages moving between chats.
* Fixed Postgres connector not being imported when bridge is compiled without
  encryption.

# v0.1.0 (2023-08-16)

Initial release.
