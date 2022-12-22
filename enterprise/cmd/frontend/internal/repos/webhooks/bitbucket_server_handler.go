package webhooks

import (
	"context"
	"net/url"
	"strings"

	"github.com/sourcegraph/log"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/webhooks"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/errcode"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/extsvc/bitbucketserver"
	"github.com/sourcegraph/sourcegraph/internal/repoupdater"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

type BitbucketServerHandler struct {
	logger log.Logger
}

func (g *BitbucketServerHandler) Register(router *webhooks.Router) {
	router.Register(func(ctx context.Context, _ database.DB, _ extsvc.CodeHostBaseURL, payload any) error {
		return g.handlePushEvent(ctx, payload)
	}, extsvc.KindBitbucketServer, "repo:refs_changed")
}

func NewBitbucketServerHandler() *BitbucketServerHandler {
	return &BitbucketServerHandler{
		logger: log.Scoped("webhooks.BitbucketServerHandler", "bitbucket server webhook handler"),
	}
}

func (g *BitbucketServerHandler) handlePushEvent(ctx context.Context, payload any) error {
	event, ok := payload.(*bitbucketserver.PushEvent)
	if !ok {
		return errors.Newf("expected BitbucketServer.PushEvent, got %T", payload)
	}

	repoName, err := bitbucketServerNameFromEvent(event)
	if err != nil {
		return errors.Wrap(err, "handlePushEvent: get name failed")
	}

	resp, err := repoupdater.DefaultClient.EnqueueRepoUpdate(ctx, repoName)
	if err != nil {
		// Repo not existing on Sourcegraph is fine
		if errcode.IsNotFound(err) {
			g.logger.Warn("BitbucketServer push event received for unknown repo", log.String("repo", string(repoName)))
			return nil
		}
		return errors.Wrap(err, "handlePushEvent: EnqueueRepoUpdate failed")
	}

	g.logger.Info("successfully updated", log.String("name", resp.Name))
	return nil
}

func bitbucketServerNameFromEvent(event *bitbucketserver.PushEvent) (api.RepoName, error) {
	if event == nil {
		return "", errors.New("nil PushEvent received")
	}
	for _, link := range event.Repository.Links.Clone {
		// The ssh link is the closest to our repo name
		if link.Name != "ssh" {
			continue
		}
		parsed, err := url.Parse(link.Href)
		if err != nil {
			return "", errors.Wrap(err, "unable to parse repository URL")
		}
		return api.RepoName(parsed.Hostname() + strings.TrimSuffix(parsed.Path, ".git")), nil
	}
	return "", errors.New("no ssh URLs found")
}