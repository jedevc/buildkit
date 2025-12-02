package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/dockerui"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/moby/buildkit/util/testutil/workers"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/tonistiigi/fsutil"
	"google.golang.org/grpc"
)

func init() {
	if workers.IsTestDockerd() {
		workers.InitDockerdWorker()
	} else {
		workers.InitOCIWorker()
		workers.InitContainerdWorker()
	}
}

func TestFrontendIntegration(t *testing.T) {
	integration.Run(t, integration.TestFuncs(
		testGatewayExternal,
		testGatewayExternalMultiplatform,
		testGatewayInternal,
	))
}

func testGatewayExternal(t *testing.T, sb integration.Sandbox) {
	workers.CheckFeatureCompat(t, sb, workers.FeatureOCIExporter, workers.FeatureOCILayout)

	c, err := client.New(sb.Context(), sb.Address())
	require.NoError(t, err)
	defer c.Close()

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	exporter := registry + "/buildkit/exporter/sample:latest"
	err = buildSampleExporter(sb.Context(), c, exporter)
	require.NoError(t, err)

	destFile := filepath.Join(t.TempDir(), "output.txt")
	fileOutput := func(map[string]string) (io.WriteCloser, error) {
		return os.Create(destFile)
	}

	destDir := filepath.Join(t.TempDir(), "outputd")

	frontend := func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		st := llb.Scratch().
			File(llb.Mkfile("/foo.txt", 0644, []byte("foo"))).
			File(llb.Mkfile("/bar.txt", 0644, []byte("bar")))
		def, err := st.Marshal(sb.Context())
		if err != nil {
			return nil, err
		}

		res, err := c.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}

		img := ocispecs.Image{
			Platform: platforms.DefaultSpec(),
			Config: ocispecs.ImageConfig{
				Labels: map[string]string{
					"testlabel": "testvalue",
				},
			},
		}
		config, err := json.Marshal(img)
		if err != nil {
			return nil, err
		}
		res.AddMeta(exptypes.ExporterImageConfigKey, config)

		return res, nil
	}

	_, err = c.Build(sb.Context(), client.SolveOpt{
		Exports: []client.ExportEntry{
			{
				Type:   "gateway",
				Output: fileOutput,
				Attrs: map[string]string{
					"opt1":   "value1",
					"source": exporter,
				},
			},
			{
				Type:      "gateway",
				OutputDir: destDir,
				Attrs: map[string]string{
					"opt1":   "value1",
					"source": exporter,
				},
			},
		},
	}, "", frontend, nil)
	require.NoError(t, err)

	destFileData, err := os.ReadFile(destFile)
	require.NoError(t, err)
	destDirData, err := os.ReadFile(destDir + "/report.json")
	require.NoError(t, err)

	fileReport := &report{}
	err = json.Unmarshal(destFileData, fileReport)
	require.NoError(t, err)
	require.Equal(t, "file", fileReport.Target)
	dirReport := &report{}
	err = json.Unmarshal(destDirData, dirReport)
	require.NoError(t, err)
	require.Equal(t, "directory", dirReport.Target)

	for _, report := range []*report{fileReport, dirReport} {
		require.Contains(t, report.Opts, "opt1")
		require.Equal(t, "value1", report.Opts["opt1"])

		require.NotEmpty(t, report.Platforms)
		require.NotEmpty(t, report.Refs)
		require.Equal(t, len(report.Platforms), len(report.Refs))

		for p, ref := range report.Refs {
			require.Contains(t, report.Platforms, p)

			var img ocispecs.Image
			err := json.Unmarshal(ref.Config, &img)
			require.NoError(t, err)
			require.Equal(t, "testvalue", img.Config.Labels["testlabel"])

			// files checked using ReadDir/ReadFile
			require.Equal(t, []string{"/bar.txt", "/foo.txt"}, ref.AllFiles)

			// layers read using content api
			require.Equal(t, 2, len(ref.Layers))
			require.Equal(t, []string{"foo.txt"}, ref.LayerFiles[ref.Layers[0]])
			require.Equal(t, []string{"bar.txt"}, ref.LayerFiles[ref.Layers[1]])
		}
	}
}

func testGatewayExternalMultiplatform(t *testing.T, sb integration.Sandbox) {
	workers.CheckFeatureCompat(t, sb, workers.FeatureOCIExporter, workers.FeatureOCILayout)

	c, err := client.New(sb.Context(), sb.Address())
	require.NoError(t, err)
	defer c.Close()

	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	exporter := registry + "/buildkit/exporter/sample-mp:latest"
	err = buildSampleExporter(sb.Context(), c, exporter)
	require.NoError(t, err)

	destFile := filepath.Join(t.TempDir(), "output.txt")
	fileOutput := func(map[string]string) (io.WriteCloser, error) {
		return os.Create(destFile)
	}

	destDir := filepath.Join(t.TempDir(), "outputd")

	platformsToTest := []string{"linux/amd64", "linux/arm64"}

	frontend := func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		st := llb.Scratch().
			File(llb.Mkfile("/foo.txt", 0644, []byte("foo"))).
			File(llb.Mkfile("/bar.txt", 0644, []byte("bar")))
		def, err := st.Marshal(sb.Context())
		if err != nil {
			return nil, err
		}

		res, err := c.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}
		ref := res.Ref

		res = gateway.NewResult()
		expPlatforms := &exptypes.Platforms{
			Platforms: make([]exptypes.Platform, len(platformsToTest)),
		}
		for i, platform := range platformsToTest {
			platformSpec := platforms.MustParse(platform)
			expPlatforms.Platforms[i] = exptypes.Platform{ID: platform, Platform: platformSpec}
			img := ocispecs.Image{
				Platform: platformSpec,
				Config: ocispecs.ImageConfig{
					Labels: map[string]string{
						"testlabel": "testvalue",
					},
				},
			}
			config, err := json.Marshal(img)
			if err != nil {
				return nil, err
			}
			res.AddRef(platform, ref)
			res.AddMeta(fmt.Sprintf("%s/%s", exptypes.ExporterImageConfigKey, platform), config)
		}
		dt, err := json.Marshal(expPlatforms)
		if err != nil {
			return nil, err
		}
		res.AddMeta(exptypes.ExporterPlatformsKey, dt)

		return res, nil
	}

	_, err = c.Build(sb.Context(), client.SolveOpt{
		Exports: []client.ExportEntry{
			{
				Type:   "gateway",
				Output: fileOutput,
				Attrs: map[string]string{
					"opt1":   "value1",
					"source": exporter,
				},
			},
			{
				Type:      "gateway",
				OutputDir: destDir,
				Attrs: map[string]string{
					"opt1":   "value1",
					"source": exporter,
				},
			},
		},
	}, "", frontend, nil)
	require.NoError(t, err)

	destFileData, err := os.ReadFile(destFile)
	require.NoError(t, err)
	destDirData, err := os.ReadFile(destDir + "/report.json")
	require.NoError(t, err)

	fileReport := &report{}
	err = json.Unmarshal(destFileData, fileReport)
	require.NoError(t, err)
	require.Equal(t, "file", fileReport.Target)
	dirReport := &report{}
	err = json.Unmarshal(destDirData, dirReport)
	require.NoError(t, err)
	require.Equal(t, "directory", dirReport.Target)

	for _, report := range []*report{fileReport, dirReport} {
		require.Contains(t, report.Opts, "opt1")
		require.Equal(t, "value1", report.Opts["opt1"])

		require.Len(t, report.Platforms, 2)
		require.Len(t, report.Refs, 2)
		require.Contains(t, report.Platforms, "linux/amd64")
		require.Contains(t, report.Platforms, "linux/arm64")

		for p, ref := range report.Refs {
			require.Contains(t, report.Platforms, p)

			var img ocispecs.Image
			err := json.Unmarshal(ref.Config, &img)
			require.NoError(t, err)
			require.Equal(t, "testvalue", img.Config.Labels["testlabel"])

			// files checked using ReadDir/ReadFile
			require.Equal(t, []string{"/bar.txt", "/foo.txt"}, ref.AllFiles)

			// layers read using content api
			require.Equal(t, 2, len(ref.Layers))
			require.Equal(t, []string{"foo.txt"}, ref.LayerFiles[ref.Layers[0]])
			require.Equal(t, []string{"bar.txt"}, ref.LayerFiles[ref.Layers[1]])
		}
	}
}

func buildSampleExporter(ctx context.Context, c *client.Client, dest string) error {
	// XXX: wild hack
	gatewayDir, err := fsutil.NewFS("/src")
	if err != nil {
		return err
	}

	exporter := dest
	_, err = c.Solve(ctx, nil, client.SolveOpt{
		Frontend: "dockerfile.v0",
		FrontendAttrs: map[string]string{
			"filename": "exporter/gateway/sample/Dockerfile",
		},
		LocalMounts: map[string]fsutil.FS{
			dockerui.DefaultLocalNameDockerfile: gatewayDir,
			dockerui.DefaultLocalNameContext:    gatewayDir,
		},
		Exports: []client.ExportEntry{
			{
				Type: client.ExporterImage,
				Attrs: map[string]string{
					"name": exporter,
					"push": "true",
				},
			},
		},
	}, nil)
	return err
}

type report struct {
	Opts      map[string]string `json:"opts"`
	Target    string            `json:"target"`
	Platforms []string          `json:"platforms"`

	Refs map[string]*reportRef `json:"refs"`
}

type reportRef struct {
	Config json.RawMessage `json:"config"`

	AllFiles []string `json:"all_files"`

	Layers     []digest.Digest            `json:"layers"`
	LayerFiles map[digest.Digest][]string `json:"layer_files"`
}

func testGatewayInternal(t *testing.T, sb integration.Sandbox) {
	workers.CheckFeatureCompat(t, sb, workers.FeatureOCIExporter, workers.FeatureOCILayout)

	c, err := client.New(sb.Context(), sb.Address())
	require.NoError(t, err)
	defer c.Close()

	frontend := func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		st := llb.Scratch().
			File(llb.Mkfile("/foo.txt", 0644, []byte("foo"))).
			File(llb.Mkfile("/bar.txt", 0644, []byte("bar")))
		def, err := st.Marshal(sb.Context())
		if err != nil {
			return nil, err
		}
		return c.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
	}

	var files []string
	export := func(ctx context.Context, c gateway.Client, _ *grpc.ClientConn, _ exptypes.ExporterTarget, result *gateway.Result) error {
		entries, err := result.Ref.ReadDir(ctx, gateway.ReadDirRequest{Path: "/"})
		if err != nil {
			return err
		}
		for _, entry := range entries {
			files = append(files, entry.Path)
		}
		return nil
	}

	_, err = c.BuildExport(sb.Context(), client.SolveOpt{}, "", frontend, export, nil)
	require.NoError(t, err)

	require.Equal(t, []string{"bar.txt", "foo.txt"}, files)
}
