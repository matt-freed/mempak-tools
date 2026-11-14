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
#include <string.h>
#include <locale.h>
#include "cli.h"

#ifndef VERSION_STR
#define VERSION_STR "unknown"
#endif

struct command {
	const char *name;
	int (*fn)(int argc, char **argv);
};

static const struct command commands[] = {
	{ "adapter", cmd_adapter },
	{ "ls", cmd_ls },
	{ "rm", cmd_rm },
	{ "extract", cmd_extract },
	{ "insert", cmd_insert },
	{ "format", cmd_format },
	{ "convert", cmd_convert },
};

static void usage(void)
{
	printf("mempak %s - N64 controller pak (mempak) tools for raphnet USB adapters\n", VERSION_STR);
	printf("\n");
	printf("Usage: mempak COMMAND [options] [args]\n");
	printf("\n");
	printf("Adapter commands (talk to the connected raphnet adapter):\n");
	printf("  adapter detect                 Show adapters, ports, controllers and mempak presence\n");
	printf("  adapter read [OUTFILE]         Read the mempak to an image file\n");
	printf("  adapter write INFILE           Write an image file to the mempak\n");
	printf("  adapter erase                  Erase the mempak\n");
	printf("\n");
	printf("Image file commands (offline, .mpk / .n64 files):\n");
	printf("  ls PAKFILE                     List notes\n");
	printf("  rm PAKFILE NOTE                Delete a note\n");
	printf("  extract PAKFILE NOTE OUTFILE   Export a note to a note file\n");
	printf("  insert PAKFILE NOTEFILE        Import a note file\n");
	printf("  format OUTFILE                 Create an empty image\n");
	printf("  convert INFILE OUTFILE         Convert between image formats\n");
	printf("\n");
	printf("  help, --help                   Show this help\n");
	printf("  version, --version             Show the version\n");
	printf("\n");
	printf("Run 'mempak COMMAND --help' (or 'mempak adapter COMMAND --help') for details.\n");
}

int main(int argc, char **argv)
{
	const char *name;
	size_t i;

	/* Needed for wide-char adapter names/serials and multibyte conversion. */
	setlocale(LC_ALL, "");

	if (argc < 2) {
		usage();
		return 2;
	}

	name = argv[1];

	if (0 == strcmp(name, "help") || 0 == strcmp(name, "-h") || 0 == strcmp(name, "--help")) {
		if (argc >= 3) {
			/* 'mempak help ls' -> 'mempak ls --help' */
			for (i = 0; i < sizeof(commands)/sizeof(commands[0]); i++) {
				if (0 == strcmp(argv[2], commands[i].name)) {
					char *sub_argv[] = { argv[2], "--help", NULL };
					return commands[i].fn(2, sub_argv);
				}
			}
		}
		usage();
		return 0;
	}

	if (0 == strcmp(name, "version") || 0 == strcmp(name, "--version")) {
		printf("mempak %s\n", VERSION_STR);
		return 0;
	}

	for (i = 0; i < sizeof(commands)/sizeof(commands[0]); i++) {
		if (0 == strcmp(name, commands[i].name)) {
			return commands[i].fn(argc - 1, argv + 1);
		}
	}

	fprintf(stderr, "Unknown command '%s'. Try 'mempak --help'.\n", name);
	return 2;
}
