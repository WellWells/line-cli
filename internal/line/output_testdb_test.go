package line

import (
	"sort"
	"strings"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// The tables line-cli reads, with the column lists of the real LINE PC schema,
// so synthetic databases behave like the real one. Rows are always synthetic.
var outSchema = map[string]string{
	"_message":            "_from TEXT, _to TEXT, _toType INTEGER, _id TEXT, _createdTime INTEGER, _deliveredTime INTEGER, _text TEXT, _location TEXT, _hasContent INTEGER, _contentType INTEGER, _contentMetadata TEXT, _contentPreview TEXT, _sessionId INTEGER, _chunks TEXT, _relatedMessageId TEXT, _messageRelationType INTEGER, _readCount INTEGER, _relatedMessageServiceCode INTEGER, _appExtensionType INTEGER, _reactions TEXT, _type INTEGER, _status INTEGER, _chatId TEXT, _reqSeqV2 INTEGER, _reqSeq INTEGER, _contentInfo TEXT, _eventInfo TEXT, _rev INTEGER, _errorCode INTEGER, _urlPreview TEXT, _hasUrlPreview INTEGER, _syncToken TEXT, _fromType INTEGER, _attribute INTEGER, _flag INTEGER, _reactionStatus TEXT, PRIMARY KEY (_id)",
	"_chat":               "_id TEXT, _midType INTEGER, _lastMessage TEXT, _lastUpdatedTime INTEGER, _sendChatCheckedId TEXT, _messageBoxNextId INTEGER, _unreadCount INTEGER, _onAir INTEGER, _showMember INTEGER, _status INTEGER, _memberHistory TEXT, _lastReceivedOpRev INTEGER, _announcement TEXT, _firstUnreadId TEXT, _messageBoxNextCreatedTime INTEGER, _lastSyncToken TEXT, _mediaMessageFlow TEXT, _messageEditNotice TEXT, PRIMARY KEY (_id)",
	"_contact":            "_mid TEXT, _createdTime INTEGER, _type INTEGER, _status INTEGER, _displayName TEXT, _displayNameOverridden TEXT, _phoneDisplayName TEXT, _pictureStatus TEXT, _thumbnailUrl TEXT, _statusMessage TEXT, _favoriteTime INTEGER, _capableVoiceCall INTEGER, _capableVideoCall INTEGER, _capableMyhome INTEGER, _capableBuddy INTEGER, _attributes INTEGER, _settings INTEGER, _picturePath TEXT, _recordId TEXT, _addedTime INTEGER, _recommendParams TEXT, _unregisteredContact INTEGER, _videoProfile TEXT, _musicProfile TEXT, _profileUpdateSign INTEGER, _statusMessageContentMetadata TEXT, _userType INTEGER, _targetProfileDetail TEXT, _friendDetail TEXT, _blockDetail TEXT, _recommendationOrNot TEXT, _notificationSettingEntry TEXT, PRIMARY KEY (_mid)",
	"_profile":            "_allowSearchByEmail INTEGER, _allowSearchByUserid INTEGER, _displayName TEXT, _email TEXT, _mid TEXT, _phone TEXT, _phoneticName TEXT, _picturePath TEXT, _pictureStatus TEXT, _regionCode TEXT, _statusMessage TEXT, _thumbnailUrl TEXT, _userid TEXT, _videoProfile TEXT, _musicProfile TEXT, _statusMessageContentMetadata TEXT, PRIMARY KEY (_mid)",
	"_groupChat":          "_type INTEGER, _chatMid TEXT, _createdTime INTEGER, _notificationDisabled INTEGER, _favoriteTimestamp INTEGER, _chatName TEXT, _picturePath TEXT, _extra TEXT, _status INTEGER, _addedTime INTEGER, _inviterMId TEXT, PRIMARY KEY (_chatMid)",
	"_squareChat":         "_squareChatMid TEXT, _squareMid TEXT, _type INTEGER, _name TEXT, _chatImageObsHash TEXT, _squareChatRevision INTEGER, _maxMemberCount INTEGER, _state INTEGER, _status TEXT, _myMember TEXT, _peerMember TEXT, _joinedAt INTEGER, _announcement TEXT, _featureSet TEXT, PRIMARY KEY (_squareChatMid)",
	"_squareMember":       "_squareMemberMid TEXT, _squareMid TEXT, _displayName TEXT, _profileImageObsHash TEXT, _ableToReceiveMessage INTEGER, _membershipState INTEGER, _role INTEGER, _revision INTEGER, _preference TEXT, _joinMessage TEXT, _singleChatId TEXT, _relation TEXT, _myMember TEXT, PRIMARY KEY (_squareMemberMid)",
	"_square":             "_mid TEXT, _name TEXT, _welcomeMessage TEXT, _profileImageObsHash TEXT, _desc TEXT, _searchable INTEGER, _type INTEGER, _categoryId INTEGER, _invitationURL TEXT, _revision INTEGER, _ableToUseInvitationTicket INTEGER, _state INTEGER, _emblems TEXT, _joinMethod TEXT, _adultOnly INTEGER, _svcTags TEXT, _createdAt INTEGER, _authority TEXT, _creatorId TEXT, _status TEXT, _myMemberId TEXT, _featureSet TEXT, _lastJoinRequestAt INTEGER, _lastVisitTime INTEGER, _noteStatus TEXT, _metadata TEXT, PRIMARY KEY (_mid)",
	"_squareToken":        "_id TEXT, _token TEXT, _backwardToken TEXT, _timestamp INTEGER, PRIMARY KEY (_id)",
	"_squareThread":       "_threadMid TEXT, _chatMid TEXT, _squareMid TEXT, _messageId TEXT, _state INTEGER, _expiresAt INTEGER, _readOnlyAt INTEGER, _revision INTEGER, _squareThreadMember TEXT, _totalMessageCount INTEGER, _unreadCount INTEGER, _lastMessage TEXT, _rootMessage TEXT, _lastMessageAt INTEGER, PRIMARY KEY (_threadMid)",
	"_room":               "_mid TEXT, _createdTime INTEGER, _contacts TEXT, _notificationDisabled INTEGER, _invitor TEXT, _memberMids TEXT, PRIMARY KEY (_mid)",
	"_chatOption":         "_id TEXT, _hidden INTEGER, _notification INTEGER, _pinnedTime INTEGER, _utilityData TEXT, PRIMARY KEY (_id)",
	"_chatE2EEPublicKeys": "_chatId TEXT, _publicKeys TEXT, _e2eeEnabled INTEGER, _allowedTypes TEXT, _specVersion INTEGER, _chatCounter INTEGER, _lastNegotiationTime INTEGER, PRIMARY KEY (_chatId)",
	"_myE2EEKeyChain":     "_mid TEXT, _e2eeKeyChain TEXT, _validation INTEGER, PRIMARY KEY (_mid)",
	"_buddyDetail":        "_mid TEXT, _memberCount INTEGER, _onAir INTEGER, _businessAccount INTEGER, _addable INTEGER, _acceptableContentTypes TEXT, _capableMyhome INTEGER, _freePhoneCallable INTEGER, _phoneNumberToDial TEXT, _needPermissionApproval INTEGER, _channelId INTEGER, _channelProviderName TEXT, _iconType INTEGER, _botType INTEGER, _showRichMenu INTEGER, _richMenuRevision INTEGER, _onAirLabel INTEGER, _onAirVersion INTEGER, _useTheme INTEGER, _themeId TEXT, _useBar INTEGER, _barRevision INTEGER, _useBackground INTEGER, _backgroundId TEXT, _statusBarEnabled INTEGER, _statusBarRevision INTEGER, _searchId TEXT, _blockable INTEGER, _botActiveStatus INTEGER, _membershipEnabled INTEGER, _legalCountryCode TEXT, _botTalkroomAds TEXT, _botOaCallDetail TEXT, _aiChatBot INTEGER, _supportSpeechToText INTEGER, _voomEnabled INTEGER, _revision INTEGER, _buddyCautionNoticeFromCMS TEXT, _region TEXT, _serviceMenuBar TEXT, _brandType INTEGER, _verifiedIconType INTEGER, _lineWorks INTEGER, PRIMARY KEY (_mid)",
}

// outDB is a synthetic, in-memory LINE database; skip names tables to leave out.
type outDB struct {
	t    *testing.T
	conn *sqlite3.Conn
	r    *Reader
}

func outTestDB(t *testing.T, skip ...string) *outDB {
	t.Helper()
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for name, cols := range outSchema {
		if !outContains(skip, name) {
			if err := conn.Exec("CREATE TABLE " + name + "(" + cols + ")"); err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
		}
	}
	return &outDB{t: t, conn: conn, r: NewReader(&DB{conn: conn})}
}

func outContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// insert adds one row; int and int64 values bind as integers, everything else as text.
func (d *outDB) insert(table string, row map[string]any) {
	d.t.Helper()
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	st, _, err := d.conn.Prepare("INSERT INTO " + table + "(" + strings.Join(cols, ",") + ") VALUES (" + placeholders(len(cols)) + ")")
	if err != nil {
		d.t.Fatalf("insert %s: %v", table, err)
	}
	defer st.Close()
	for i, c := range cols {
		switch v := row[c].(type) {
		case int:
			err = st.BindInt64(i+1, int64(v))
		case int64:
			err = st.BindInt64(i+1, v)
		case string:
			err = st.BindText(i+1, v)
		default:
			d.t.Fatalf("unsupported value for %s", c)
		}
		if err != nil {
			d.t.Fatal(err)
		}
	}
	if err := st.Exec(); err != nil {
		d.t.Fatalf("insert %s: %v", table, err)
	}
}

// chat adds a chat row and, for a "c..." id, a named group.
func (d *outDB) chat(id string) {
	d.insert("_chat", map[string]any{"_id": id, "_lastUpdatedTime": int64(1_700_000_000_000), "_unreadCount": 0})
	if strings.HasPrefix(id, "c") {
		d.insert("_groupChat", map[string]any{"_chatMid": id, "_chatName": "group-" + id[:4]})
	}
}

// msg adds a message; base fields can be overridden by extra.
func (d *outDB) msg(id, chatID string, createdMs int64, ct int, extra map[string]any) {
	row := map[string]any{
		"_id": id, "_chatId": chatID, "_from": "u-sender-1", "_toType": 2, "_createdTime": createdMs,
		"_contentType": ct, "_type": 1, "_attribute": 0, "_text": "", "_contentInfo": "", "_contentMetadata": "",
	}
	for k, v := range extra {
		row[k] = v
	}
	d.insert("_message", row)
}
