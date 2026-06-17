package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIsMigrationChecksumCompatible_UpgradeCompatCases(t *testing.T) {
	cases := []struct {
		name         string
		migration    string
		dbChecksum   string
		fileChecksum string
	}{
		{
			name:         "014",
			migration:    "014_drop_legacy_allowed_groups.sql",
			dbChecksum:   "b63fc37ba6a6ffdcb50e082250be5f34f40632597d568bb97247c607198befcc",
			fileChecksum: "f939e24bde1904b3253acd743f75618845892e039dcc78b72916fded8fb81263",
		},
		{
			name:         "019",
			migration:    "019_migrate_wechat_to_attributes.sql",
			dbChecksum:   "d45e05b4bb722b287377790583c2677b8666dbf7e02b626c93468491d4ce8cf8",
			fileChecksum: "f0798a7c381f85eccbd437a2f661f9d8b25ca91de332885ea4760fd299417914",
		},
		{
			name:         "033",
			migration:    "033_ops_monitoring_vnext.sql",
			dbChecksum:   "accf363544d187aecad4f1c68fe34118f86d1a931465e66490c530d3f3f1106d",
			fileChecksum: "727d91efba866d26cd7e34fe84ca72434bd82dfd65d7548ceaf03fd58be757e2",
		},
		{
			name:         "054",
			migration:    "054_drop_legacy_cache_columns.sql",
			dbChecksum:   "82de761156e03876653e7a6a4eee883cd927847036f779b0b9f34c42a8af7a7d",
			fileChecksum: "8061ef96e52b5e8d65c18a05c10899f649cd636d78cb576c6fed6330c31f607e",
		},
		{
			name:         "090",
			migration:    "090_drop_sora.sql",
			dbChecksum:   "5326d418f2ccc10ce8f4c8766ae490eccbc96c29ba76ea5807635985bbfb877c",
			fileChecksum: "9dc538a1ee4981bf733e62e147c6837a2f0497ef1e73646dea60f827017b7522",
		},
		{
			name:         "127",
			migration:    "127_drop_channel_monitor_deleted_at.sql",
			dbChecksum:   "ac7decb355555a711a372e1ed0d7f1559af41cd796e4be1736f4f978f5f88735",
			fileChecksum: "0d7336a4e12ee8b01b93d2ceb3c9f44a5523f01dbcd215efe47f897448472e4d",
		},
		{
			name:         "133",
			migration:    "133_allow_email_oauth_provider_types.sql",
			dbChecksum:   "1d5948a0de5fae4fff368e4ae648edb794ce4e7b2704b6e7020c3b881c0f51f3",
			fileChecksum: "21e0bfe39530adea594e7d161d202f2dd201cc8caffeeb8b563ada15ab7f8fc5",
		},
		{
			name:         "135",
			migration:    "135_allow_email_oauth_provider_types.sql",
			dbChecksum:   "e5e3512fd7ff6e9225414bf79425fd8ddbf6d78a66998142bfb2441f8e7e4708",
			fileChecksum: "72b6005ff64cddd845615e249d95858010dd6d3bd7a1d24b3e9a32eb03b76eb8",
		},
		{
			name:         "142",
			migration:    "142_remove_ops_retry_replay_compat.sql",
			dbChecksum:   "88d139daf730c511ffcd9e8ce99769a6dbc781d63a8b1b954f272254c9468c6f",
			fileChecksum: "e8f288ca3a703457c47cce4d697f36a35da230914efc3d57ecb25d4b4842e643",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, ok := migrationChecksumCompatibilityRules[tc.migration]
			require.True(t, ok)
			require.Equal(t, tc.fileChecksum, rule.fileChecksum)
			require.True(t, isMigrationChecksumCompatible(tc.migration, tc.dbChecksum, tc.fileChecksum))
		})
	}
}

func TestMigrationChecksumCompatibilityRules_MatchEmbeddedCurrentFiles(t *testing.T) {
	for migration, rule := range migrationChecksumCompatibilityRules {
		if migration != "014_drop_legacy_allowed_groups.sql" &&
			migration != "019_migrate_wechat_to_attributes.sql" &&
			migration != "033_ops_monitoring_vnext.sql" &&
			migration != "054_drop_legacy_cache_columns.sql" &&
			migration != "090_drop_sora.sql" &&
			migration != "127_drop_channel_monitor_deleted_at.sql" &&
			migration != "133_allow_email_oauth_provider_types.sql" &&
			migration != "135_allow_email_oauth_provider_types.sql" &&
			migration != "142_remove_ops_retry_replay_compat.sql" {
			continue
		}

		t.Run(migration, func(t *testing.T) {
			content, err := fs.ReadFile(migrations.FS, migration)
			require.NoError(t, err)
			sum := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
			require.Equal(t, rule.fileChecksum, hex.EncodeToString(sum[:]))
		})
	}
}
