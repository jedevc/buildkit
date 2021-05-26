package instructions

import (
	"bytes"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/strslice"
	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/require"
)

func TestCommandsExactlyOneArgument(t *testing.T) {
	commands := []string{
		"MAINTAINER",
		"WORKDIR",
		"USER",
		"STOPSIGNAL",
	}

	for _, cmd := range commands {
		ast, err := parser.Parse(strings.NewReader(cmd))
		require.NoError(t, err)
		_, err = ParseInstruction(ast.AST.Children[0])
		require.EqualError(t, err, errExactlyOneArgument(cmd).Error())
	}
}

func TestCommandsAtLeastOneArgument(t *testing.T) {
	commands := []string{
		"ENV",
		"LABEL",
		"ONBUILD",
		"HEALTHCHECK",
		"EXPOSE",
		"VOLUME",
	}

	for _, cmd := range commands {
		ast, err := parser.Parse(strings.NewReader(cmd))
		require.NoError(t, err)
		_, err = ParseInstruction(ast.AST.Children[0])
		require.EqualError(t, err, errAtLeastOneArgument(cmd).Error())
	}
}

func TestCommandsNoDestinationArgument(t *testing.T) {
	commands := []string{
		"ADD",
		"COPY",
	}

	for _, cmd := range commands {
		ast, err := parser.Parse(strings.NewReader(cmd + " arg1"))
		require.NoError(t, err)
		_, err = ParseInstruction(ast.AST.Children[0])
		require.EqualError(t, err, errNoDestinationArgument(cmd).Error())
	}
}

func TestCommandsTooManyArguments(t *testing.T) {
	commands := []string{
		"ENV",
		"LABEL",
	}

	for _, command := range commands {
		node := &parser.Node{
			Original: command + "arg1 arg2 arg3",
			Value:    strings.ToLower(command),
			Next: &parser.Node{
				Value: "arg1",
				Next: &parser.Node{
					Value: "arg2",
					Next: &parser.Node{
						Value: "arg3",
					},
				},
			},
		}
		_, err := ParseInstruction(node)
		require.EqualError(t, err, errTooManyArguments(command).Error())
	}
}

func TestCommandsBlankNames(t *testing.T) {
	commands := []string{
		"ENV",
		"LABEL",
	}

	for _, cmd := range commands {
		node := &parser.Node{
			Original: cmd + " =arg2",
			Value:    strings.ToLower(cmd),
			Next: &parser.Node{
				Value: "",
				Next: &parser.Node{
					Value: "arg2",
				},
			},
		}
		_, err := ParseInstruction(node)
		require.EqualError(t, err, errBlankCommandNames(cmd).Error())
	}
}

func TestHealthCheckCmd(t *testing.T) {
	node := &parser.Node{
		Value: command.Healthcheck,
		Next: &parser.Node{
			Value: "CMD",
			Next: &parser.Node{
				Value: "hello",
				Next: &parser.Node{
					Value: "world",
				},
			},
		},
	}
	cmd, err := ParseInstruction(node)
	require.NoError(t, err)
	hc, ok := cmd.(*HealthCheckCommand)
	require.Equal(t, true, ok)
	expected := []string{"CMD-SHELL", "hello world"}
	require.Equal(t, expected, hc.Health.Test)
}

func TestParseOptInterval(t *testing.T) {
	flInterval := &Flag{
		name:     "interval",
		flagType: stringType,
		Value:    "50ns",
	}
	_, err := parseOptInterval(flInterval)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be less than 1ms")

	flInterval.Value = "1ms"
	_, err = parseOptInterval(flInterval)
	require.NoError(t, err)
}

func TestCommentsDetection(t *testing.T) {
	dt := `# foo sets foo
ARG foo=bar

# base defines first stage
FROM busybox AS base
# this is irrelevant
ARG foo
# bar defines bar
# baz is something else
ARG bar baz=123
`

	ast, err := parser.Parse(bytes.NewBuffer([]byte(dt)))
	require.NoError(t, err)

	stages, meta, err := Parse(ast.AST)
	require.NoError(t, err)

	require.Equal(t, "defines first stage", stages[0].Comment)
	require.Equal(t, "foo", meta[0].Args[0].Key)
	require.Equal(t, "sets foo", meta[0].Args[0].Comment)

	st := stages[0]

	require.Equal(t, "foo", st.Commands[0].(*ArgCommand).Args[0].Key)
	require.Equal(t, "", st.Commands[0].(*ArgCommand).Args[0].Comment)
	require.Equal(t, "bar", st.Commands[1].(*ArgCommand).Args[0].Key)
	require.Equal(t, "defines bar", st.Commands[1].(*ArgCommand).Args[0].Comment)
	require.Equal(t, "baz", st.Commands[1].(*ArgCommand).Args[1].Key)
	require.Equal(t, "is something else", st.Commands[1].(*ArgCommand).Args[1].Comment)
}

func TestErrorCases(t *testing.T) {
	cases := []struct {
		name          string
		dockerfile    string
		expectedError string
	}{
		{
			name: "copyEmptyWhitespace",
			dockerfile: `COPY	
		quux \
      bar`,
			expectedError: "COPY requires at least two arguments",
		},
		{
			name:          "COPY heredoc destination",
			dockerfile:    "COPY /foo <<EOF\nEOF",
			expectedError: "COPY cannot accept a heredoc as a destination",
		},
		{
			name:          "COPY heredoc extra args",
			dockerfile:    "COPY <<EOF /foo /bar\nEOF",
			expectedError: "COPY requires exactly two arguments",
		},
		{
			name:          "ONBUILD forbidden FROM",
			dockerfile:    "ONBUILD FROM scratch",
			expectedError: "FROM isn't allowed as an ONBUILD trigger",
		},
		{
			name:          "MAINTAINER unknown flag",
			dockerfile:    "MAINTAINER --boo joe@example.com",
			expectedError: "Unknown flag: boo",
		},
		{
			name:          "Chaining ONBUILD",
			dockerfile:    `ONBUILD ONBUILD RUN touch foobar`,
			expectedError: "Chaining ONBUILD via `ONBUILD ONBUILD` isn't allowed",
		},
		{
			name:          "RUN heredoc with extra args",
			dockerfile:    "RUN <<EOF invalid\nEOF",
			expectedError: "RUN with heredoc requires exactly one argument",
		},
		{
			name:          "Invalid instruction",
			dockerfile:    `foo bar`,
			expectedError: "unknown instruction: FOO",
		},
		{
			name:          "Invalid instruction",
			dockerfile:    `foo bar`,
			expectedError: "unknown instruction: FOO",
		},
	}
	for _, c := range cases {
		r := strings.NewReader(c.dockerfile)
		ast, err := parser.Parse(r)

		if err != nil {
			t.Fatalf("Error when parsing Dockerfile: %s", err)
		}
		n := ast.AST.Children[0]
		_, err = ParseInstruction(n)
		require.Error(t, err)
		require.Contains(t, err.Error(), c.expectedError)
	}
}

func TestRunCmdFlagsUsed(t *testing.T) {
	dockerfile := "RUN --mount=type=tmpfs,target=/foo/ echo hello"
	r := strings.NewReader(dockerfile)
	ast, err := parser.Parse(r)
	require.NoError(t, err)

	n := ast.AST.Children[0]
	c, err := ParseInstruction(n)
	require.NoError(t, err)
	require.IsType(t, c, &RunCommand{})
	require.Equal(t, []string{"mount"}, c.(*RunCommand).FlagsUsed)
}

func TestCopyHeredoc(t *testing.T) {
	cases := []struct {
		dockerfile    string
		content       []string
		preventExpand bool
	}{
		{
			dockerfile: "COPY /foo /bar",
			content:    nil,
		},
		{
			dockerfile: "COPY <<EOF /bar\nEOF",
			content:    []string{},
		},
		{
			dockerfile: "COPY <<EOF /bar\nTESTING\nEOF",
			content:    []string{"TESTING"},
		},
		{
			dockerfile:    "COPY <<'EOF' /bar\nTESTING\nEOF",
			content:       []string{"TESTING"},
			preventExpand: true,
		},
	}

	for _, c := range cases {
		r := strings.NewReader(c.dockerfile)
		ast, err := parser.Parse(r)
		require.NoError(t, err)

		n := ast.AST.Children[0]
		comm, err := ParseInstruction(n)
		require.NoError(t, err)
		require.Equal(t, c.content, comm.(*CopyCommand).Content)
		require.Equal(t, c.preventExpand, comm.(*CopyCommand).PreventExpand)
	}
}

func TestRunHeredoc(t *testing.T) {
	cases := []struct {
		dockerfile string
		commands   []strslice.StrSlice
		shell      bool
	}{
		{
			dockerfile: "RUN ls /",
			commands:   []strslice.StrSlice{{"ls /"}},
			shell:      true,
		},
		{
			dockerfile: `RUN ["ls", "/"]`,
			commands:   []strslice.StrSlice{{"ls", "/"}},
			shell:      false,
		},
		{
			dockerfile: "RUN [\"<<EOF\"]\nls /\nEOF",
			commands:   []strslice.StrSlice{{"ls /"}},
			shell:      true,
		},
		{
			dockerfile: "RUN <<EOF\nls /\nwhoami\nEOF",
			commands:   []strslice.StrSlice{{"ls /"}, {"whoami"}},
			shell:      true,
		},
	}

	for _, c := range cases {
		r := strings.NewReader(c.dockerfile)
		ast, err := parser.Parse(r)
		require.NoError(t, err)

		n := ast.AST.Children[0]
		comm, err := ParseInstruction(n)
		require.NoError(t, err)
		require.Equal(t, c.commands, comm.(*RunCommand).CmdLines)
	}
}
