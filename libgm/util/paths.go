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

var MESSAGING = INSTANT_MESSAGING + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
var RECEIVE_MESSAGES = MESSAGING + "/ReceiveMessages"
var SEND_MESSAGE = MESSAGING + "/SendMessage"
var ACK_MESSAGES = MESSAGING + "/AckMessages"

var TENOR_BASE_URL = "https://api.tenor.com/v1"
var TENOR_SEARCH_GIF = TENOR_BASE_URL + "/search"
