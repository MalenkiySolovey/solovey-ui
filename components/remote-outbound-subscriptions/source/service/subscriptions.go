//go:build !minimal

package remotesubservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
)

type Settings interface {
	GetRemoteGroupAdaptation() (string, error)
	GetRemoteConversionPolicy() (string, error)
}

type Service struct {
	Store         Store
	Effects       Effects
	Security      SourceSecurity
	Fetcher       SourceFetcher
	Checks        ConnectionChecker
	LogWarning    func(...any)
	Settings      Settings
	ProbeOutbound func(context.Context, string, string, time.Duration) ProbeResult
}

func (s *Service) GetAll() (*[]remotesub.RemoteOutboundSubscription, error) {
	db := s.Store
	if err := db.Transaction(func(tx Store) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.BackfillGroupMemberships(tx); err != nil {
			return err
		}
		return remotesub.ReconcileGroupStates(tx)
	}); err != nil {
		return nil, err
	}
	var subscriptions []remotesub.RemoteOutboundSubscription
	err := db.ListSubscriptionDetails(&subscriptions)
	if err != nil {
		return nil, err
	}
	if err := remotesub.HydrateConnectionGroupIDs(db, subscriptions); err != nil {
		return nil, err
	}
	remotesub.FilterVisibleConnections(subscriptions)
	remotesub.HydrateConnectionTypeInfo(subscriptions)
	for i := range subscriptions {
		if err := s.Security.ProjectSourceContent(&subscriptions[i]); err != nil {
			return nil, err
		}
	}
	return &subscriptions, nil
}
func (s *Service) SaveSubscription(input remotesub.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*remotesub.RemoteOutboundSubscription, error) {
	now := time.Now().Unix()
	input.Name = strings.TrimSpace(input.Name)
	input.Url = strings.TrimSpace(input.Url)
	input.SourcePath = strings.TrimSpace(input.SourcePath)
	if err := remotesub.ValidateSourceUserAgent(input.UserAgent); err != nil {
		return nil, err
	}
	input.UserAgent = strings.TrimSpace(input.UserAgent)
	if policy, err := s.Security.PrepareSourceHealthCheck(input.HealthCheck); err != nil {
		return nil, err
	} else {
		input.HealthCheck = policy
	}
	if prepared, err := s.Security.PrepareDialerOverride(input.DialerOverride); err != nil {
		return nil, err
	} else {
		input.DialerOverride = prepared
	}
	input.TagPrefix = remotesub.SanitizeTagPrefix(input.TagPrefix)
	input.UpdateInterval = remotesub.NormalizeUpdateInterval(input.UpdateInterval)
	if input.Name == "" {
		return nil, serviceError("subscription name can not be empty")
	}
	if len([]rune(input.Name)) > 120 {
		return nil, serviceError("subscription name is too long")
	}
	input.SourceType = input.SourceKind()
	if input.SourceType != remotesub.SourceRemote && input.SourceType != remotesub.SourceInline && input.SourceType != remotesub.SourceFile {
		return nil, serviceError("SUBSCRIPTION_SOURCE_TYPE_INVALID")
	}
	if err := input.ValidateSourceFilters(); err != nil {
		return nil, err
	}
	if input.DownloadOutboundID != 0 && input.SourceType != remotesub.SourceRemote {
		return nil, serviceError("SUBSCRIPTION_DOWNLOAD_OUTBOUND_REMOTE_ONLY")
	}
	if input.SourceType == remotesub.SourceInline {
		if input.UserAgent != "" {
			return nil, serviceError("INLINE_SUBSCRIPTION_FETCH_OPTIONS_NOT_ALLOWED")
		}
		if input.Url != "" {
			return nil, serviceError("INLINE_SUBSCRIPTION_URL_NOT_ALLOWED")
		}
		input.AutoUpdate = false
	}
	if input.SourceType == remotesub.SourceRemote {
		if input.Url == "" {
			return nil, serviceError("subscription url can not be empty")
		}
		if len(input.Url) > 8192 {
			return nil, serviceError("subscription url is too long")
		}
		if err := s.Security.ValidateSubscriptionURL(input.Url); err != nil {
			return nil, serviceError("subscription url is not allowed: ", err)
		}
	}
	if input.SourceType == remotesub.SourceFile {
		if input.Url != "" || input.UserAgent != "" {
			return nil, serviceError("FILE_SUBSCRIPTION_FETCH_OPTIONS_NOT_ALLOWED")
		}
		if err := s.Security.ValidateSourceFilePath(input.SourcePath); err != nil {
			return nil, err
		}
	} else if input.SourcePath != "" {
		return nil, serviceError("SUBSCRIPTION_SOURCE_FILE_PATH_NOT_ALLOWED")
	}
	if input.TagPrefix == "" {
		input.TagPrefix = remotesub.DefaultTagPrefix(input.Name, input.Id)
	}
	if !enabledProvided && input.Id == 0 {
		input.Enabled = true
	}
	// Settings reads use the admitted database outside the write transaction.
	// Do not open a second connection while holding an inline-content save.
	options := remotesub.FetchOptions{}
	if input.SourceType == remotesub.SourceInline {
		options = s.fetchOptions()
	}
	db := s.Store
	var saved remotesub.RemoteOutboundSubscription
	coreRestart := false
	err := db.Transaction(func(tx Store) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.ReconcileGroupStates(tx); err != nil {
			return err
		}
		sortOrder, err := tx.SubscriptionSaveOrder(input.Id)
		if err != nil {
			return err
		}
		if input.Id != 0 {
			if err := tx.LoadSubscription(input.Id, &saved); err != nil {
				return err
			}
			if saved.SourceKind() != input.SourceType {
				return serviceError("SUBSCRIPTION_SOURCE_TYPE_IMMUTABLE")
			}
			if err := tx.ValidateDownloadOutbound(input.DownloadOutboundID, saved.DownloadOutboundID); err != nil {
				return err
			}
			oldTagPrefix := saved.TagPrefix
			saved.Name = input.Name
			saved.Url = input.Url
			saved.UserAgent = input.UserAgent
			saved.DownloadOutboundID = input.DownloadOutboundID
			saved.SourcePath = input.SourcePath
			saved.Include = input.Include
			saved.Exclude = input.Exclude
			saved.RemoveEmojis = input.RemoveEmojis
			saved.DialerOverride = input.DialerOverride
			if len(input.HealthCheck) != 0 {
				saved.HealthCheck = input.HealthCheck
			}
			saved.Enabled = input.Enabled
			saved.TagPrefix = input.TagPrefix
			saved.AutoUpdate = input.AutoUpdate
			saved.UpdateInterval = input.UpdateInterval
			saved.SortOrder = sortOrder
			saved.UpdatedAt = now
			if oldTagPrefix != saved.TagPrefix {
				changed, err := remotesub.RetagSubscriptionConnections(tx, &saved)
				if err != nil {
					return err
				}
				coreRestart = changed
			}
		} else {
			if err := tx.ValidateDownloadOutbound(input.DownloadOutboundID, 0); err != nil {
				return err
			}
			saved = remotesub.RemoteOutboundSubscription{
				SortOrder:          sortOrder,
				Name:               input.Name,
				Url:                input.Url,
				UserAgent:          input.UserAgent,
				DownloadOutboundID: input.DownloadOutboundID,
				SourceType:         input.SourceType,
				SourcePath:         input.SourcePath,
				Include:            input.Include,
				Exclude:            input.Exclude,
				RemoveEmojis:       input.RemoveEmojis,
				DialerOverride:     input.DialerOverride,
				HealthCheck:        input.HealthCheck,
				Enabled:            input.Enabled,
				TagPrefix:          input.TagPrefix,
				AutoUpdate:         input.AutoUpdate,
				UpdateInterval:     input.UpdateInterval,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
		}
		if err := tx.SaveSubscription(&saved); err != nil {
			return err
		}
		if err := s.Security.ApplySourceContent(tx.Context(), tx, &saved, input.Content, options); err != nil {
			return err
		}
		if err := s.Security.ApplySourceHeaders(tx, &saved, input.FetchHeaders); err != nil {
			return err
		}
		if err := tx.SaveSubscription(&saved); err != nil {
			return err
		}
		if strings.Contains(saved.TagPrefix, "{id}") {
			saved.TagPrefix = strings.ReplaceAll(saved.TagPrefix, "{id}", fmt.Sprintf("%d", saved.Id))
			if err := tx.SaveSubscription(&saved); err != nil {
				return err
			}
		}
		if _, err := remotesub.EnsureDefaultGroup(tx, saved.Id, now); err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "subscription_save", saved)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	loaded, err := s.GetSubscription(saved.Id)
	if err != nil {
		return nil, err
	}
	return loaded, nil
}
func (s *Service) GetSubscription(id uint) (*remotesub.RemoteOutboundSubscription, error) {
	var subscription remotesub.RemoteOutboundSubscription
	err := s.Store.LoadSubscriptionDetails(id, &subscription)
	if err != nil {
		return nil, err
	}
	subscriptions := []remotesub.RemoteOutboundSubscription{subscription}
	if err := remotesub.HydrateConnectionGroupIDs(s.Store, subscriptions); err != nil {
		return nil, err
	}
	remotesub.FilterVisibleConnections(subscriptions)
	remotesub.HydrateConnectionTypeInfo(subscriptions)
	subscription = subscriptions[0]
	if err := s.Security.ProjectSourceContent(&subscription); err != nil {
		return nil, err
	}
	return &subscription, nil
}
func (s *Service) DeleteSubscription(id uint, loginUser string) error {
	if id == 0 {
		return serviceError("subscription id is required")
	}
	coreRestart := false
	err := s.Store.Transaction(func(tx Store) error {
		var subscription remotesub.RemoteOutboundSubscription
		if err := tx.LoadSubscription(id, &subscription); err != nil {
			return err
		}
		outboundChanged, err := remotesub.DeleteSyncedOutboundsForSubscription(tx, subscription.Id)
		if err != nil {
			return err
		}
		coreRestart = coreRestart || outboundChanged
		if err := s.recordRemoteOutboundChange(tx, loginUser, "subscription_delete", subscription); err != nil {
			return err
		}
		if err := tx.DeleteSubscriptionMemberships(subscription.Id); err != nil {
			return err
		}
		if err := tx.DeleteSubscriptionConnections(subscription.Id); err != nil {
			return err
		}
		if err := tx.DeleteSubscriptionGroups(subscription.Id); err != nil {
			return err
		}
		return tx.DeleteSubscription(subscription.Id)
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return nil
}
func (s *Service) RefreshSubscriptionContext(ctx context.Context, id uint, loginUser string) (*remotesub.RefreshResult, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	return s.refreshSubscription(ctx, id, loginUser)
}
func (s *Service) refreshSubscription(ctx context.Context, id uint, loginUser string) (*remotesub.RefreshResult, error) {
	if id == 0 {
		return nil, serviceError("subscription id is required")
	}
	var subscription remotesub.RemoteOutboundSubscription
	db := s.Store
	ctx = db.OperationContext(ctx)
	db = db.WithContext(ctx)
	if err := db.LoadSubscription(id, &subscription); err != nil {
		return nil, err
	}
	verifySource := func(tx Store) error {
		var current remotesub.RemoteOutboundSubscription
		if err := tx.LoadSubscription(id, &current); err != nil {
			return err
		}
		if !subscription.SameSource(current) {
			return serviceError("SUBSCRIPTION_SOURCE_CHANGED_RETRY")
		}
		return nil
	}
	fetched, fetchErr := s.Fetcher.FetchSource(ctx, db, subscription, s.fetchOptions())
	now := time.Now().Unix()
	if fetchErr != nil {
		// An old failed fetch has no authority over a replacement source's UI
		// state, just as an old successful fetch cannot replace its collection.
		err := db.Transaction(func(tx Store) error {
			if err := verifySource(tx); err != nil {
				return err
			}
			return remotesub.MarkRefreshError(tx, id, fetchErr.Error(), now)
		})
		if err != nil {
			return nil, err
		}
		return nil, fetchErr
	}
	var result remotesub.RefreshResult
	syncedChanged := false
	err := db.Transaction(func(tx Store) error {
		if err := verifySource(tx); err != nil {
			return err
		}
		refreshResult, coreChanged, err := remotesub.RefreshFetchedSubscription(tx, &subscription, fetched, now)
		if err != nil {
			return err
		}
		result = refreshResult
		syncedChanged = coreChanged
		return s.recordRemoteOutboundChange(tx, loginUser, "subscription_refresh", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, syncedChanged)
	return &result, nil
}

func (s *Service) fetchOptions() remotesub.FetchOptions {
	if s.Settings == nil {
		return remotesub.FetchOptions{ConversionPolicy: subconversion.DefaultPolicy()}
	}
	groupAdaptation, err := s.Settings.GetRemoteGroupAdaptation()
	if err != nil {
		groupAdaptation = ""
	}
	rawPolicy, err := s.Settings.GetRemoteConversionPolicy()
	if err != nil {
		rawPolicy = ""
	}
	return remotesub.FetchOptions{
		GroupAdaptation:  groupAdaptation,
		ConversionPolicy: subconversion.ParsePolicy(rawPolicy, groupAdaptation),
	}
}

func (s *Service) SyncConnectionToOutbound(id uint, loginUser string) (*remotesub.RemoteOutboundConnection, error) {
	if id == 0 {
		return nil, serviceError("connection id is required")
	}
	var saved remotesub.RemoteOutboundConnection
	err := s.Store.Transaction(func(tx Store) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.ReconcileGroupStates(tx); err != nil {
			return err
		}
		if err := tx.LoadConnection(id, &saved); err != nil {
			return err
		}
		if err := remotesub.SyncConnectionToOutbound(tx, &saved, true); err != nil {
			return err
		}
		if err := tx.SaveConnection(&saved); err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "connection_sync", saved)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, true)
	return &saved, nil
}
func (s *Service) recordRemoteOutboundChange(tx Store, loginUser string, act string, payload any) error {
	if s.Effects == nil {
		return serviceError("remote outbound effects are not configured")
	}
	return s.Effects.RecordChange(tx, loginUser, act, payload)
}
func (s *Service) invalidateRemoteOutboundData(loginUser string, coreRestart bool) {
	if s.Effects != nil {
		s.Effects.Invalidate(loginUser, coreRestart)
	}
}
