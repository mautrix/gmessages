package util

var MESSAGES_GOOGLE_BASE_URL = "https://messages.google.com"

var MESSAGES_GOOGLE_AUTHENTICATION = MESSAGES_GOOGLE_BASE_URL + "/web/authentication"
var MESSAGES_GOOGLE_TIMESOURCE = MESSAGES_GOOGLE_BASE_URL + "/web/timesource"

var INSTANT_MESSAGING = "https://instantmessaging-pa.googleapis.com"

var UPLOAD_MEDIA = INSTANT_MESSAGING + "/upload"

var PAIRING = INSTANT_MESSAGING + "/$rpc/google.internal.communications.instantmessaging.v1.Pairing"
var REGISTER_PHONE_RELAY = PAIRING + "/RegisterPhoneRelay"
var REFRESH_PHONE_RELAY = PAIRING + "/RefreshPhoneRelay"
var GET_WEB_ENCRYPTION_KEY = PAIRING + "/GetWebEncryptionKey"
var REVOKE_RELAY_PAIRING = PAIRING + "/RevokeRelayPairing"

var MESSAGING = INSTANT_MESSAGING + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
var RECEIVE_MESSAGES = MESSAGING + "/ReceiveMessages"
var SEND_MESSAGE = MESSAGING + "/SendMessage"
var ACK_MESSAGES = MESSAGING + "/AckMessages"

var REGISTRATION = INSTANT_MESSAGING + "/$rpc/google.internal.communications.instantmessaging.v1.Registration"
var REGISTER_REFRESH = REGISTRATION + "/RegisterRefresh"

var CONFIG_URL = "https://messages.google.com/web/config"
