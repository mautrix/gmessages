package util

const MessagesBaseURL = "https://messages.google.com"

const GoogleAuthenticationURL = MessagesBaseURL + "/web/authentication"
const GoogleTimesourceURL = MessagesBaseURL + "/web/timesource"

const instantMessagingBaseURL = "https://instantmessaging-pa.googleapis.com"
const instantMessagingBaseURLGoogle = "https://instantmessaging-pa.clients6.google.com"

const UploadMediaURL = instantMessagingBaseURL + "/upload"

const pairingBaseURL = instantMessagingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Pairing"
const RegisterPhoneRelayURL = pairingBaseURL + "/RegisterPhoneRelay"
const RefreshPhoneRelayURL = pairingBaseURL + "/RefreshPhoneRelay"
const GetWebEncryptionKeyURL = pairingBaseURL + "/GetWebEncryptionKey"
const RevokeRelayPairingURL = pairingBaseURL + "/RevokeRelayPairing"

const messagingBaseURL = instantMessagingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
const messagingBaseURLGoogle = instantMessagingBaseURLGoogle + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
const ReceiveMessagesURL = messagingBaseURL + "/ReceiveMessages"
const SendMessageURL = messagingBaseURL + "/SendMessage"
const AckMessagesURL = messagingBaseURL + "/AckMessages"
const ReceiveMessagesURLGoogle = messagingBaseURLGoogle + "/ReceiveMessages"
const SendMessageURLGoogle = messagingBaseURLGoogle + "/SendMessage"
const AckMessagesURLGoogle = messagingBaseURLGoogle + "/AckMessages"

const registrationBaseURL = instantMessagingBaseURLGoogle + "/$rpc/google.internal.communications.instantmessaging.v1.Registration"
const SignInGaiaURL = registrationBaseURL + "/SignInGaia"
const RegisterRefreshURL = registrationBaseURL + "/RegisterRefresh"

const ConfigURL = "https://messages.google.com/web/config"
