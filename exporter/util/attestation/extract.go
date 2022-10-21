package attestation

import (
	"context"
	"encoding/json"
	"os"
	"path"

	"github.com/containerd/continuity/fs"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/moby/buildkit/cache"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/snapshot"
	"github.com/moby/buildkit/solver/result"
	"github.com/moby/buildkit/util/purl"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type SubjectOpt struct {
	ImageNames []string
	Digest     digest.Digest
	Platform   *ocispecs.Platform
}

func (opt *SubjectOpt) PURLs() ([]string, error) {
	purls := make([]string, len(opt.ImageNames))
	for i, name := range opt.ImageNames {
		if name == "" {
			continue
		}
		p, err := purl.RefToPURL(name, opt.Platform)
		if err != nil {
			return nil, err
		}
		purls[i] = p
	}
	return purls, nil
}

func Extract(ctx context.Context, s session.Group, refs map[string]cache.ImmutableRef, attestations []result.Attestation, defaultSubject *SubjectOpt) ([]intoto.Statement, error) {
	eg, ctx := errgroup.WithContext(ctx)
	statements := make([]intoto.Statement, len(attestations))

	for i, att := range attestations {
		i, att := i, att
		eg.Go(func() error {
			content := att.Content
			if content == nil {
				if refs == nil {
					return errors.Errorf("no refs map provided to lookup attestation keys")
				}
				ref, ok := refs[att.Ref]
				if !ok {
					return errors.Errorf("key %s not found in refs map", att.Ref)
				}
				mount, err := ref.Mount(ctx, true, s)
				if err != nil {
					return err
				}
				lm := snapshot.LocalMounter(mount)
				src, err := lm.Mount()
				if err != nil {
					return err
				}
				defer lm.Unmount()

				p, err := fs.RootPath(src, att.Path)
				if err != nil {
					return err
				}
				content, err = os.ReadFile(p)
				if err != nil {
					return errors.Wrap(err, "cannot read in-toto attestation")
				}
			}
			if len(content) == 0 {
				content = nil
			}

			switch att.Kind {
			case gatewaypb.AttestationKindInToto:
				stmt, err := extractInToto(ctx, content, att, defaultSubject)
				if err != nil {
					return err
				}
				statements[i] = *stmt
			case gatewaypb.AttestationKindBundle:
				return errors.New("bundle attestation kind must be un-bundled first")
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return statements, nil
}

func extractInToto(ctx context.Context, content []byte, attestation result.Attestation, defaultSubject *SubjectOpt) (*intoto.Statement, error) {
	if len(attestation.InToto.Subjects) == 0 {
		attestation.InToto.Subjects = []result.InTotoSubject{{
			Kind: gatewaypb.InTotoSubjectKindSelf,
		}}
	}
	subjects := []intoto.Subject{}
	for _, subject := range attestation.InToto.Subjects {
		subjectName := "_"
		if subject.Name != "" {
			subjectName = subject.Name
		}

		switch subject.Kind {
		case gatewaypb.InTotoSubjectKindSelf:
			if defaultSubject != nil {
				subjectNames := []string{}
				if subjectName != "_" {
					subjectNames = append(subjectNames, subjectName)
				}
				purls, err := defaultSubject.PURLs()
				if err != nil {
					return nil, err
				}
				subjectNames = append(subjectNames, purls...)

				for _, name := range subjectNames {
					subjects = append(subjects, intoto.Subject{
						Name:   name,
						Digest: result.ToDigestMap(defaultSubject.Digest),
					})
				}
			}
		case gatewaypb.InTotoSubjectKindRaw:
			subjects = append(subjects, intoto.Subject{
				Name:   subjectName,
				Digest: result.ToDigestMap(subject.Digest...),
			})
		default:
			return nil, errors.Errorf("unknown attestation subject type %T", subject)
		}
	}

	stmt := intoto.Statement{
		StatementHeader: intoto.StatementHeader{
			Type:          intoto.StatementInTotoV01,
			PredicateType: attestation.InToto.PredicateType,
			Subject:       subjects,
		},
		Predicate: json.RawMessage(content),
	}
	return &stmt, nil
}

func Unbundle(ctx context.Context, s session.Group, refs map[string]cache.ImmutableRef, bundled []result.Attestation) ([]result.Attestation, error) {
	eg, ctx := errgroup.WithContext(ctx)
	unbundled := make([][]result.Attestation, len(bundled))

	for i, att := range bundled {
		i, att := i, att
		eg.Go(func() error {
			switch att.Kind {
			case gatewaypb.AttestationKindInToto:
				unbundled[i] = append(unbundled[i], att)
			case gatewaypb.AttestationKindBundle:
				if refs == nil {
					return errors.Errorf("no refs map provided to lookup attestation keys")
				}
				ref, ok := refs[att.Ref]
				if !ok {
					return errors.Errorf("key %s not found in refs map", att.Ref)
				}

				mount, err := ref.Mount(ctx, true, s)
				if err != nil {
					return err
				}
				lm := snapshot.LocalMounter(mount)
				src, err := lm.Mount()
				if err != nil {
					return err
				}
				defer lm.Unmount()

				atts, err := unbundle(ctx, src, att)
				if err != nil {
					return err
				}
				unbundled[i] = append(unbundled[i], atts...)
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	var joined []result.Attestation
	for _, atts := range unbundled {
		joined = append(joined, atts...)
	}
	return joined, nil
}

func unbundle(ctx context.Context, root string, bundle result.Attestation) ([]result.Attestation, error) {
	dir, err := fs.RootPath(root, bundle.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var unbundled []result.Attestation
	for _, entry := range entries {
		p, err := fs.RootPath(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(f)
		var stmt intoto.Statement
		if err := dec.Decode(&stmt); err != nil {
			return nil, errors.Wrap(err, "cannot decode in-toto statement")
		}
		if bundle.InToto.PredicateType != "" && stmt.PredicateType != bundle.InToto.PredicateType {
			return nil, errors.Errorf("bundle entry %s does not match required predicate type %s", stmt.PredicateType, bundle.InToto.PredicateType)
		}

		predicate, err := json.Marshal(stmt.Predicate)
		if err != nil {
			return nil, err
		}

		subjects := make([]result.InTotoSubject, len(stmt.Subject))
		for i, subject := range stmt.Subject {
			subjects[i] = result.InTotoSubject{
				Kind:   gatewaypb.InTotoSubjectKindRaw,
				Name:   subject.Name,
				Digest: result.FromDigestMap(subject.Digest),
			}
		}
		unbundled = append(unbundled, result.Attestation{
			Kind:    gatewaypb.AttestationKindInToto,
			Path:    path.Join(bundle.Path, entry.Name()),
			Content: predicate,
			InToto: result.InTotoAttestation{
				PredicateType: stmt.PredicateType,
				Subjects:      subjects,
			},
		})
	}
	return unbundled, nil
}
