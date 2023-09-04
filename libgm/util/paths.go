package util

const messagesBaseURL = "https://messages.google.com"

const GoogleAuthenticationURL = messagesBaseURL + "/web/authentication"
const GoogleTimesourceURL = messagesBaseURL + "/web/timesource"

const instantMessangingBaseURL = "https://instantmessaging-pa.googleapis.com"

const UploadMediaURL = instantMessangingBaseURL + "/upload"

const pairingBaseURL = instantMessangingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Pairing"
const RegisterPhoneRelayURL = pairingBaseURL + "/RegisterPhoneRelay"
const RefreshPhoneRelayURL = pairingBaseURL + "/RefreshPhoneRelay"
const GetWebEncryptionKeyURL = pairingBaseURL + "/GetWebEncryptionKey"
const RevokeRelayPairingURL = pairingBaseURL + "/RevokeRelayPairing"

const messagingBaseURL = instantMessangingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
const ReceiveMessagesURL = messagingBaseURL + "/ReceiveMessages"
const SendMessageURL = messagingBaseURL + "/SendMessage"
const AckMessagesURL = messagingBaseURL + "/AckMessages"

const registrationBaseURL = instantMessangingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Registration"
const RegisterRefreshURL = registrationBaseURL + "/RegisterRefresh"

const ConfigURL = "https://messages.google.com/web/config"
