/*	mempak : N64 controller pak tools for raphnet adapters
	Based on gcn64tools, Copyright (C) 2007-2017  Raphael Assenat <raph@raphnet.net>

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include "cli.h"
#include "mempak.h"

static const cli_opt_t *find_long(const cli_opt_t *opts, int n_opts, const char *name, size_t len)
{
	int i;

	for (i = 0; i < n_opts; i++) {
		if (opts[i].longname && strlen(opts[i].longname) == len &&
				0 == strncmp(opts[i].longname, name, len)) {
			return &opts[i];
		}
	}
	return NULL;
}

static const cli_opt_t *find_short(const cli_opt_t *opts, int n_opts, char c)
{
	int i;

	for (i = 0; i < n_opts; i++) {
		if (opts[i].shortname && opts[i].shortname == c) {
			return &opts[i];
		}
	}
	return NULL;
}

int cli_parse(int argc, char **argv, const cli_opt_t *opts, int n_opts, char **pos, int *n_pos)
{
	int i;
	int only_positional = 0;

	*n_pos = 0;

	for (i = 1; i < argc; i++) {
		char *arg = argv[i];
		const cli_opt_t *opt = NULL;
		const char *inline_val = NULL;

		if (only_positional || arg[0] != '-' || arg[1] == 0) {
			if (*n_pos >= CLI_MAX_POSITIONAL) {
				fprintf(stderr, "Too many arguments\n");
				return -1;
			}
			pos[(*n_pos)++] = arg;
			continue;
		}

		if (0 == strcmp(arg, "--")) {
			only_positional = 1;
			continue;
		}

		if (arg[1] == '-') {
			const char *name = arg + 2;
			const char *eq = strchr(name, '=');
			size_t len = eq ? (size_t)(eq - name) : strlen(name);

			opt = find_long(opts, n_opts, name, len);
			if (!opt) {
				fprintf(stderr, "Unknown option '%s'\n", arg);
				return -1;
			}
			if (eq) {
				inline_val = eq + 1;
			}
		} else {
			opt = find_short(opts, n_opts, arg[1]);
			if (!opt) {
				fprintf(stderr, "Unknown option '%s'\n", arg);
				return -1;
			}
			if (arg[2]) {
				if (!opt->value) {
					fprintf(stderr, "Option '-%c' does not take a value (combined short options are not supported)\n", arg[1]);
					return -1;
				}
				inline_val = arg + 2;
			}
		}

		if (opt->value) {
			if (inline_val) {
				*opt->value = inline_val;
			} else {
				if (i + 1 >= argc) {
					fprintf(stderr, "Option '%s' requires a value\n", arg);
					return -1;
				}
				*opt->value = argv[++i];
			}
		} else {
			if (inline_val) {
				fprintf(stderr, "Option '%s' does not take a value\n", arg);
				return -1;
			}
			if (opt->count) {
				(*opt->count)++;
			}
		}
	}

	return 0;
}

int cli_parse_uint(const char *what, const char *str, int max, int *dst)
{
	char *end;
	long val;

	errno = 0;
	val = strtol(str, &end, 0);

	if (errno || end == str || *end || val < 0 || val > max) {
		fprintf(stderr, "Invalid %s '%s' (expected 0-%d)\n", what, str, max);
		return -1;
	}

	*dst = (int)val;
	return 0;
}

/* atoi() maps anything unparsable to 0, which would silently target note 0. */
int cli_parse_note(const char *str, int *dst)
{
	return cli_parse_uint("note slot", str, 15, dst);
}

int cli_confirm(const char *question)
{
	char buf[16];

	fprintf(stderr, "%s [y/N] ", question);
	fflush(stderr);

	if (!fgets(buf, sizeof(buf), stdin)) {
		return 0;
	}

	return buf[0] == 'y' || buf[0] == 'Y';
}

int cli_resolve_format(const char *format_opt, const char *outfile)
{
	int fmt;

	if (format_opt) {
		fmt = mempak_string2format(format_opt);
		if (fmt == MPK_FORMAT_INVALID) {
			fprintf(stderr, "Unknown format '%s'. Use mpk, mpk4 or n64.\n", format_opt);
		}
		return fmt;
	}

	/* Note: mempak_getFilenameFormat() maps .mpk to the 128kB MPK4 layout;
	 * we deliberately default to the standard 32kB image instead. */
	if (outfile && mempak_getFilenameFormat(outfile) == MPK_FORMAT_N64) {
		return MPK_FORMAT_N64;
	}

	return mempak_string2format(DEFAULT_FORMAT_STR);
}

void cli_print_formats(void)
{
	printf("Formats:\n");
	printf("  mpk     Standard 32kB raw image (default)\n");
	printf("  mpk4    128kB raw image (4 copies of the 32kB image)\n");
	printf("  n64     DexDrive image (supports note comments; default for *.n64)\n");
}
