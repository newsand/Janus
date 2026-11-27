# Graph Report - .  (2026-11-26)

## Corpus Check
- 34 files · ~58,678 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 423 nodes · 1138 edges · 25 communities detected
- Extraction: 100% EXTRACTED · 0% INFERRED · 0% AMBIGUOUS
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Community 0|Community 0]]
- [[_COMMUNITY_Community 1|Community 1]]
- [[_COMMUNITY_Community 2|Community 2]]
- [[_COMMUNITY_Community 3|Community 3]]
- [[_COMMUNITY_Community 4|Community 4]]
- [[_COMMUNITY_Community 5|Community 5]]
- [[_COMMUNITY_Community 6|Community 6]]
- [[_COMMUNITY_Community 7|Community 7]]
- [[_COMMUNITY_Community 8|Community 8]]
- [[_COMMUNITY_Community 9|Community 9]]
- [[_COMMUNITY_Community 10|Community 10]]
- [[_COMMUNITY_Community 11|Community 11]]
- [[_COMMUNITY_Community 12|Community 12]]
- [[_COMMUNITY_Community 13|Community 13]]
- [[_COMMUNITY_Community 14|Community 14]]
- [[_COMMUNITY_Community 15|Community 15]]
- [[_COMMUNITY_Community 16|Community 16]]
- [[_COMMUNITY_Community 17|Community 17]]
- [[_COMMUNITY_Community 18|Community 18]]
- [[_COMMUNITY_Community 19|Community 19]]
- [[_COMMUNITY_Community 20|Community 20]]
- [[_COMMUNITY_Community 21|Community 21]]
- [[_COMMUNITY_Community 22|Community 22]]
- [[_COMMUNITY_Community 23|Community 23]]
- [[_COMMUNITY_Community 24|Community 24]]

## God Nodes (most connected - your core abstractions)
1. `charUsersSetup()` - 30 edges
2. `charUsersDo()` - 30 edges
3. `charSetup()` - 25 edges
4. `charUsersBearer()` - 23 edges
5. `setupTestDB()` - 22 edges
6. `t33Serve()` - 21 edges
7. `charDo()` - 21 edges
8. `t32Setup()` - 18 edges
9. `t32Do()` - 18 edges
10. `t32CreateUser()` - 17 edges

## Surprising Connections (you probably didn't know these)
- `TestRefreshTokenRotation()` --calls--> `setupTestDB()`  [EXTRACTED]
  features/auth/handler_test.go → features/users/handler_test.go
- `TestRefreshTokenReuseRevokesFamily()` --calls--> `setupTestDB()`  [EXTRACTED]
  features/auth/handler_test.go → features/users/handler_test.go
- `TestRefreshDisabledUserRejected()` --calls--> `setupTestDB()`  [EXTRACTED]
  features/auth/handler_test.go → features/users/handler_test.go
- `TestLoginDisabledUserRejected()` --calls--> `setupTestDB()`  [EXTRACTED]
  features/auth/handler_test.go → features/users/handler_test.go
- `TestLoginFailuresAreIndistinguishable()` --calls--> `setupTestDB()`  [EXTRACTED]
  features/auth/handler_test.go → features/users/handler_test.go

## Communities

### Community 0 - "Community 0"
Cohesion: 0.21
Nodes (40): charUsersActiveRefresh(), charUsersBearer(), charUsersCreateInvite(), charUsersCreateRefresh(), charUsersCreateUser(), charUsersDo(), charUsersExpectError(), charUsersReload() (+32 more)

### Community 1 - "Community 1"
Cohesion: 0.23
Nodes (34): charAssertTokenResponse(), charCountRefresh(), charDecode(), charDo(), charEnable2FA(), charExpectBody(), charInsertMagic(), charKeys() (+26 more)

### Community 2 - "Community 2"
Cohesion: 0.24
Nodes (29): t33AssertUnauthorized(), t33Claims(), t33ErrorBody(), t33Serve(), t33SignToken(), t33UseConfig(), t33UseJWTSecret(), t33UseServiceKeys() (+21 more)

### Community 3 - "Community 3"
Cohesion: 0.34
Nodes (28): t32AccessToken(), t32AssertError(), t32Body(), t32CreateChallenge(), t32CreateUser(), t32Do(), t32ReloadUser(), t32SecretPtr() (+20 more)

### Community 4 - "Community 4"
Cohesion: 0.21
Nodes (24): createTestUser(), loginRequest(), setupTestDB(), setupTestRouter(), setupTestRouterWithAuth(), TestAcceptInviteAllowsPendingUserWithoutPassword(), TestAcceptInviteCreatesNewUser(), TestAcceptInviteRejectsExistingAccountWithPassword() (+16 more)

### Community 5 - "Community 5"
Cohesion: 0.26
Nodes (23): UpdateAPIKeyRequest, UpdateAPIKeyResponse, assertKeyUnchanged(), assertUpdateError(), insertUpdateTestKey(), patchUpdateAPIKey(), reloadUpdateTestKey(), TestUpdateAPIKeyDivergentAudienceReturns404WithoutEffect() (+15 more)

### Community 6 - "Community 6"
Cohesion: 0.1
Nodes (17): AcceptInvite(), CreateUser(), Enable(), FindUsableToken(), findUserOr404(), generateTOTPSecret(), GetUser(), RegisterRoutes() (+9 more)

### Community 7 - "Community 7"
Cohesion: 0.21
Nodes (22): CreateAPIKeyRequest, createAPIKeyResponse, bindCreateAPIKeyRequest(), CreateAPIKey(), expiresAt(), assertExpiresAround(), countAPIKeys(), createBody() (+14 more)

### Community 8 - "Community 8"
Cohesion: 0.24
Nodes (18): ListAPIKeysQuery, ListAPIKeysResponse, assertListedIDs(), assertNoSecretsInListJSON(), getListAPIKeys(), insertListTestKey(), listedIDs(), TestListAPIKeysAudienceInjectionIsTreatedAsLiteral() (+10 more)

### Community 9 - "Community 9"
Cohesion: 0.16
Nodes (18): LoginRequest, MagicLinkConsumeRequest, MagicLinkRequest, RefreshRequest, ConsumeMagicLink(), createMagicToken(), createTwoFAChallenge(), CurrentUser() (+10 more)

### Community 10 - "Community 10"
Cohesion: 0.27
Nodes (17): RevokeAPIKeyRequest, RevokeAPIKeyResponse, assertKeyStillActive(), assertRevokeNotFound(), insertRevokeTestKey(), postRevokeAPIKey(), reloadRevokeTestKey(), TestRevokeAPIKeyAlreadyRevokedKeepsPastRevokedAt() (+9 more)

### Community 11 - "Community 11"
Cohesion: 0.27
Nodes (18): t33AssertStatus(), t33Get(), t33RateLimitedEngine(), t33UseLockout(), t33UseRateLimit(), TestT33_Lockout_AfterDuration_UnlocksAndRestartsCount(), TestT33_Lockout_ClearLockout_UnlocksAndRestartsCount(), TestT33_Lockout_ClearUnknownIdentifier_IsNoop() (+10 more)

### Community 12 - "Community 12"
Cohesion: 0.12
Nodes (7): APIKey, Invite, MagicToken, RecoverToken, RefreshToken, TwoFAChallenge, User

### Community 13 - "Community 13"
Cohesion: 0.21
Nodes (7): isolateConfig(), TestT30LoadAndGetAreSingleton(), TestT30LoadDefaultsFailValidateOutsideDev(), TestT30LoadDefaultsWhenEnvEmpty(), TestT30LoadInvalidValuesFallBackToDefaults(), TestT30LoadReadsValidEnvValues(), TestT30ValidateFromEnvDevPathAndProdPath()

### Community 14 - "Community 14"
Cohesion: 0.51
Nodes (12): insertVerifyTestKey(), newVerifyRawKey(), postVerifyAPIKey(), TestVerifyAPIKeyDoesNotWrite(), TestVerifyAPIKeyEveryInvalidReasonReturnsIdentical401(), TestVerifyAPIKeyInvalidRequestReturns400(), TestVerifyAPIKeyNotYetExpiredKeyReturns200(), TestVerifyAPIKeyRawKeyNeverInStdout() (+4 more)

### Community 15 - "Community 15"
Cohesion: 0.4
Nodes (8): Config, Get(), getBoolEnv(), getDurationEnv(), getEnv(), getIntEnv(), Load(), parseServiceKeys()

### Community 16 - "Community 16"
Cohesion: 0.36
Nodes (7): Debug(), Error(), Fatal(), Info(), Level, log(), Warn()

### Community 17 - "Community 17"
Cohesion: 0.57
Nodes (7): ownerUUIDAssert400(), ownerUUIDBody(), ownerUUIDCountKeys(), ownerUUIDPost(), ownerUUIDSetupDBWithoutUsers(), TestCreateAPIKeyEmptyOrMissingOwnerStillReturns400InvalidRequest(), TestCreateAPIKeyNonUUIDOwnerReturns400InvalidOwner()

### Community 18 - "Community 18"
Cohesion: 0.29
Nodes (2): lockoutEntry, rateLimitEntry

### Community 19 - "Community 19"
Cohesion: 0.53
Nodes (4): Close(), DB(), HealthCheck(), Init()

### Community 20 - "Community 20"
Cohesion: 0.4
Nodes (5): verifiedAPIKey, VerifyAPIKeyRequest, VerifyAPIKeyResponse, findValidAPIKey(), VerifyAPIKey()

### Community 21 - "Community 21"
Cohesion: 0.6
Nodes (4): bearerToken(), JWTAuth(), ServiceKeyAuth(), Claims

### Community 22 - "Community 22"
Cohesion: 0.7
Nodes (4): apiKeyIDParam(), changeScopedAPIKey(), mutateScopedAPIKey(), respondAPIKeyMutationError()

### Community 23 - "Community 23"
Cohesion: 1.0
Nodes (2): main(), requestLogger()

### Community 24 - "Community 24"
Cohesion: 1.0
Nodes (0): 

## Knowledge Gaps
- **24 isolated node(s):** `rateLimitEntry`, `lockoutEntry`, `Claims`, `t33Seen`, `Level` (+19 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `Community 24`** (2 nodes): `TestValidate()`, `config_test.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `RegisterRoutes()` connect `Community 6` to `Community 9`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._
- **What connects `rateLimitEntry`, `lockoutEntry`, `Claims` to the rest of the system?**
  _24 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 6` be split into smaller, more focused modules?**
  _Cohesion score 0.1 - nodes in this community are weakly interconnected._
- **Should `Community 12` be split into smaller, more focused modules?**
  _Cohesion score 0.12 - nodes in this community are weakly interconnected._