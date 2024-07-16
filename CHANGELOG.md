# v0.4.3 (2024-07-16)

* Added support for new protocol version in Google account pairing.
* Added support for handling messages being modified, e.g. full-res media
  arriving later than the thumbnail.
  * This may or may not cover actual RCS edits if/when those are rolled out.

# v0.4.2 (2024-06-16)

* Added error message if phone doesn't send echo for outgoing message in
  time.
* Added better error messages for some message send failures.
* Added logging for RPC request and response IDs.
* Fixed sending messages incorrectly forcing RCS in some cases causing failures
  (e.g. when using dual SIM and sending from a SIM with RCS disabled).
* Fixed ping loop getting stuck (and therefore not keeping the connection
  alive) if the first ping never responds.
* Removed unnecessary sleep after Google account pairing.

# v0.4.1 (2024-05-16)

* Added support for sending captions.
  * Note that RCS doesn't support captions yet, so sending captions in RCS
    chats will cause weirdness. Captions should work in MMS chats.
* Fixed frequent disconnections when using Google account pairing with an
  email containing uppercase characters.
* Fixed some cases of spam messages being bridged even after Google's filter
  caught them.

# v0.4.0 (2024-04-16)

* Added automatic detection and workarounds for cases where the app stops
  sending new messages to the bridge.
* Improved participant deduplication and extended it to cover groups too
  instead of only DMs.
* Fixed some cases of Google account pairing not working correctly.
* Fixed database errors related to ghosts after switching phones or clearing
  data on phone (caused by the ghost avatar fix in 0.3.0).

# v0.3.0 (2024-03-16)

* Bumped minimum Go version to 1.21.
* Added support for pairing via Google account.
  * See [docs](https://docs.mau.fi/bridges/go/gmessages/authentication.html)
    for instructions.
  * There are no benefits to using this method, it still requires your phone to
    be online. Google Fi cloud sync is still not supported.
* Added deduplication for DM participants, as Google randomly sends duplicate
  participant entries sometimes.
* Added voice message conversion.
* Changed custom image reactions to be bridged as `:custom:` instead of a UUID.
  Google Messages for Web doesn't support fetching the actual image yet.
* Fixed sending reactions breaking for some users.
* Fixed ghost user avatars not being reset properly when switching phones or
  clearing data on phone.

# v0.2.4 (2024-01-16)

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
