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
#ifndef _cli_h__
#define _cli_h__

/* One accepted option. If 'value' is non-NULL the option takes an argument
 * which is stored there; otherwise '*count' is incremented each time the
 * option is seen. */
typedef struct cli_opt {
	char shortname;          /* 0 if none */
	const char *longname;    /* NULL if none */
	const char **value;
	int *count;
} cli_opt_t;

#define CLI_MAX_POSITIONAL	8

/**
 * Parse argv (argv[0] is the command name and is skipped).
 *
 * Accepts -x, -x VALUE, -xVALUE, --name, --name VALUE, --name=VALUE and '--'
 * to end option processing. Options and positionals may be interleaved.
 *
 * Positional arguments are stored in pos[] (at most CLI_MAX_POSITIONAL).
 *
 * \return 0 on success, -1 on error (message already printed).
 */
int cli_parse(int argc, char **argv, const cli_opt_t *opts, int n_opts, char **pos, int *n_pos);

/** Parse a note/slot number (0-15). Prints an error and returns -1 if invalid. */
int cli_parse_note(const char *str, int *dst);

/** Parse a non-negative integer bounded by max. Prints an error and returns -1 if invalid. */
int cli_parse_uint(const char *what, const char *str, int max, int *dst);

#define DEFAULT_FORMAT_STR	"mpk"

/** Pick an output image format: explicit -f wins, then a .n64 extension,
 * otherwise the default (32kB raw mpk). Returns MPK_FORMAT_INVALID (and prints
 * an error) for an unknown -f value. */
int cli_resolve_format(const char *format_opt, const char *outfile);
void cli_print_formats(void);

/** Ask a y/N question on stderr. Returns 1 for yes. */
int cli_confirm(const char *question);

int cmd_adapter(int argc, char **argv);

int cmd_ls(int argc, char **argv);
int cmd_rm(int argc, char **argv);
int cmd_extract(int argc, char **argv);
int cmd_insert(int argc, char **argv);
int cmd_format(int argc, char **argv);
int cmd_convert(int argc, char **argv);

#endif // _cli_h__
