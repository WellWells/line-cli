package line

import "github.com/ncruces/go-sqlite3"

// syntheticSchema has the column lists of the LINE PC tables line-cli reads.
const syntheticSchema = `
CREATE TABLE _message (_from TEXT, _to TEXT, _toType INTEGER, _id TEXT, _createdTime INTEGER, _deliveredTime INTEGER, _text TEXT, _location TEXT, _hasContent INTEGER, _contentType INTEGER, _contentMetadata TEXT, _contentPreview TEXT, _sessionId INTEGER, _chunks TEXT, _relatedMessageId TEXT, _messageRelationType INTEGER, _readCount INTEGER, _relatedMessageServiceCode INTEGER, _appExtensionType INTEGER, _reactions TEXT, _type INTEGER, _status INTEGER, _chatId TEXT, _reqSeqV2 INTEGER, _reqSeq INTEGER, _contentInfo TEXT, _eventInfo TEXT, _rev INTEGER, _errorCode INTEGER, _urlPreview TEXT, _hasUrlPreview INTEGER, _syncToken TEXT, _fromType INTEGER, _attribute INTEGER, _flag INTEGER, _reactionStatus TEXT, PRIMARY KEY (_id));
CREATE TABLE _chat (_id TEXT, _midType INTEGER, _lastMessage TEXT, _lastUpdatedTime INTEGER, _sendChatCheckedId TEXT, _messageBoxNextId INTEGER, _unreadCount INTEGER, _onAir INTEGER, _showMember INTEGER, _status INTEGER, _memberHistory TEXT, _lastReceivedOpRev INTEGER, _announcement TEXT, _firstUnreadId TEXT, _messageBoxNextCreatedTime INTEGER, _lastSyncToken TEXT, _mediaMessageFlow TEXT, _messageEditNotice TEXT, PRIMARY KEY (_id));
CREATE TABLE _contact (_mid TEXT, _createdTime INTEGER, _type INTEGER, _status INTEGER, _displayName TEXT, _displayNameOverridden TEXT, _phoneDisplayName TEXT, _pictureStatus TEXT, _thumbnailUrl TEXT, _statusMessage TEXT, _favoriteTime INTEGER, _capableVoiceCall INTEGER, _capableVideoCall INTEGER, _capableMyhome INTEGER, _capableBuddy INTEGER, _attributes INTEGER, _settings INTEGER, _picturePath TEXT, _recordId TEXT, _addedTime INTEGER, _recommendParams TEXT, _unregisteredContact INTEGER, _videoProfile TEXT, _musicProfile TEXT, _profileUpdateSign INTEGER, _statusMessageContentMetadata TEXT, _userType INTEGER, _targetProfileDetail TEXT, _friendDetail TEXT, _blockDetail TEXT, _recommendationOrNot TEXT, _notificationSettingEntry TEXT, PRIMARY KEY (_mid));
CREATE TABLE _profile (_allowSearchByEmail INTEGER, _allowSearchByUserid INTEGER, _displayName TEXT, _email TEXT, _mid TEXT, _phone TEXT, _phoneticName TEXT, _picturePath TEXT, _pictureStatus TEXT, _regionCode TEXT, _statusMessage TEXT, _thumbnailUrl TEXT, _userid TEXT, _videoProfile TEXT, _musicProfile TEXT, _statusMessageContentMetadata TEXT, PRIMARY KEY (_mid));
CREATE TABLE _groupChat (_type INTEGER, _chatMid TEXT, _createdTime INTEGER, _notificationDisabled INTEGER, _favoriteTimestamp INTEGER, _chatName TEXT, _picturePath TEXT, _extra TEXT, _status INTEGER, _addedTime INTEGER, _inviterMId TEXT, PRIMARY KEY (_chatMid));
CREATE TABLE _room (_mid TEXT, _createdTime INTEGER, _contacts TEXT, _notificationDisabled INTEGER, _invitor TEXT, _memberMids TEXT, PRIMARY KEY (_mid));
CREATE TABLE _squareChat (_squareChatMid TEXT, _squareMid TEXT, _type INTEGER, _name TEXT, _chatImageObsHash TEXT, _squareChatRevision INTEGER, _maxMemberCount INTEGER, _state INTEGER, _status TEXT, _myMember TEXT, _peerMember TEXT, _joinedAt INTEGER, _announcement TEXT, _featureSet TEXT, PRIMARY KEY (_squareChatMid));
CREATE TABLE _squareMember (_squareMemberMid TEXT, _squareMid TEXT, _displayName TEXT, _profileImageObsHash TEXT, _ableToReceiveMessage INTEGER, _membershipState INTEGER, _role INTEGER, _revision INTEGER, _preference TEXT, _joinMessage TEXT, _singleChatId TEXT, _relation TEXT, _myMember TEXT, PRIMARY KEY (_squareMemberMid));
CREATE TABLE _square (_mid TEXT, _name TEXT, _welcomeMessage TEXT, _profileImageObsHash TEXT, _desc TEXT, _searchable INTEGER, _type INTEGER, _categoryId INTEGER, _invitationURL TEXT, _revision INTEGER, _ableToUseInvitationTicket INTEGER, _state INTEGER, _emblems TEXT, _joinMethod TEXT, _adultOnly INTEGER, _svcTags TEXT, _createdAt INTEGER, _authority TEXT, _creatorId TEXT, _status TEXT, _myMemberId TEXT, _featureSet TEXT, _lastJoinRequestAt INTEGER, _lastVisitTime INTEGER, _noteStatus TEXT, _metadata TEXT, PRIMARY KEY (_mid));
CREATE TABLE _squareThread (_threadMid TEXT, _chatMid TEXT, _squareMid TEXT, _messageId TEXT, _state INTEGER, _expiresAt INTEGER, _readOnlyAt INTEGER, _revision INTEGER, _squareThreadMember TEXT, _totalMessageCount INTEGER, _unreadCount INTEGER, _lastMessage TEXT, _rootMessage TEXT, _lastMessageAt INTEGER, PRIMARY KEY (_threadMid));
CREATE TABLE _squareToken (_id TEXT, _token TEXT, _backwardToken TEXT, _timestamp INTEGER, PRIMARY KEY (_id));
CREATE TABLE _chatOption (_id TEXT, _hidden INTEGER, _notification INTEGER, _pinnedTime INTEGER, _utilityData TEXT, PRIMARY KEY (_id));
CREATE TABLE _buddyDetail (_mid TEXT, _memberCount INTEGER, _onAir INTEGER, _businessAccount INTEGER, _addable INTEGER, _acceptableContentTypes TEXT, _capableMyhome INTEGER, _freePhoneCallable INTEGER, _phoneNumberToDial TEXT, _needPermissionApproval INTEGER, _channelId INTEGER, _channelProviderName TEXT, _iconType INTEGER, _botType INTEGER, _showRichMenu INTEGER, _richMenuRevision INTEGER, _onAirLabel INTEGER, _onAirVersion INTEGER, _useTheme INTEGER, _themeId TEXT, _useBar INTEGER, _barRevision INTEGER, _useBackground INTEGER, _backgroundId TEXT, _statusBarEnabled INTEGER, _statusBarRevision INTEGER, _searchId TEXT, _blockable INTEGER, _botActiveStatus INTEGER, _membershipEnabled INTEGER, _legalCountryCode TEXT, _botTalkroomAds TEXT, _botOaCallDetail TEXT, _aiChatBot INTEGER, _supportSpeechToText INTEGER, _voomEnabled INTEGER, _revision INTEGER, _buddyCautionNoticeFromCMS TEXT, _region TEXT, _serviceMenuBar TEXT, _brandType INTEGER, _verifiedIconType INTEGER, _lineWorks INTEGER, PRIMARY KEY (_mid));
CREATE INDEX _message__chatId__createdTime ON _message (_chatId, _createdTime, _contentType, _status);
`

// OpenSynthetic opens a private in-memory database with LINE's table layout and
// runs script on it. Tests use it to run commands on made-up rows without LINE.
func OpenSynthetic(script string) (*DB, error) {
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		return nil, err
	}
	if err := conn.Exec(syntheticSchema + script); err != nil {
		conn.Close()
		return nil, err
	}
	return &DB{conn: conn}, nil
}
