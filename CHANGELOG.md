# v0.2.0 (unreleased)

* Added support for double puppeting with arbitrary `as_token`s.
  See [docs](https://docs.mau.fi/bridges/general/double-puppeting.html#appservice-method-new) for more info.
* Switched to "tablet mode", to allow using the bridge in parallel with Messages for Web.
  * You can have at least two tablets and one web session simultaneously. The
    bridge will now take one tablet slot by default, but you can change the
    device type in the bridge config.
  * Existing users will have to log out and re-pair the bridge to switch to
    tablet mode.
* Fixed incoming message and conversation data sometimes going into the wrong
  portals.
* Fixed bridge sometimes getting immediately logged out after pairing.
* Fixed reactions not being saved to the database.
* Fixed Postgres connector not being imported when bridge is compiled without
  encryption.

# v0.1.0 (2023-08-16)

Initial release.
