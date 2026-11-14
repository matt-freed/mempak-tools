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

/* Offline commands: operate on mempak image files only, never on the adapter.
 * Logic ported from gcn64tools' mempak_ls/rm/extract_note/insert_note/format/convert. */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cli.h"
#include "mempak.h"

#define MEMPAK_TOTAL_BLOCKS	123
#define ARRAY_SIZE(a) (sizeof(a)/sizeof(a[0]))



/********************************* ls *********************************/

/* Note names come back as "NAME<padding>.EXT" with spaces padding both
 * parts (and an empty extension for most games). Turn that into "NAME" or
 * "NAME.EXT". */
static void format_note_name(const char *raw, char *dst, size_t dst_size)
{
	char buf[128];
	char *dot, *end;
	const char *ext = "";

	snprintf(buf, sizeof(buf), "%s", raw);

	dot = strrchr(buf, '.');
	if (dot) {
		*dot = 0;
		ext = dot + 1;
		while (*ext == ' ')
			ext++;
		end = (char *)ext + strlen(ext);
		while (end > ext && end[-1] == ' ')
			*--end = 0;
	}

	end = buf + strlen(buf);
	while (end > buf && end[-1] == ' ')
		*--end = 0;

	if (*ext) {
		snprintf(dst, dst_size, "%s.%s", buf, ext);
	} else {
		snprintf(dst, dst_size, "%s", buf);
	}
}

static void print_code(const unsigned char *code, int len)
{
	int i, printable = 1;

	for (i = 0; i < len; i++) {
		if (code[i] < 0x20 || code[i] > 0x7e)
			printable = 0;
	}

	for (i = 0; i < len; i++) {
		if (printable) {
			putchar(code[i]);
		} else {
			printf("%02x", code[i]);
		}
	}
}

static void usage_ls(void)
{
	printf("Usage: mempak ls [options] PAKFILE\n");
	printf("\n");
	printf("List the notes (saves) stored in a mempak image.\n");
	printf("Raw (.mpk, .bin) and DexDrive (.n64) images are accepted.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -v, --verbose   Also show game code, publisher code and region\n");
	printf("  -h, --help      Show this help\n");
}

int cmd_ls(int argc, char **argv)
{
	int help = 0, verbose = 0;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos, note, res, retval = 0;
	mempak_structure_t *mpk;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
		{ 'v', "verbose", NULL, &verbose },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_ls();
		return 0;
	}
	if (n_pos != 1) {
		usage_ls();
		return 2;
	}

	mpk = mempak_loadFromFile(pos[0]);
	if (!mpk) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}

	printf("Image: %s (%s format)\n", pos[0], mempak_format2string(mpk->file_format));

	if (0 != validate_mempak(mpk)) {
		printf("Mempak invalid (not formatted or corrupted)\n");
		mempak_free(mpk);
		return 1;
	}

	printf("Blocks used: %d / %d\n", MEMPAK_TOTAL_BLOCKS - get_mempak_free_space(mpk), MEMPAK_TOTAL_BLOCKS);
	printf("\n");

	for (note = 0; note < MEMPAK_NUM_NOTES; note++) {
		entry_structure_t note_data;
		char name[sizeof(note_data.utf8_name)];

		printf("Note %2d: ", note);
		res = get_mempak_entry(mpk, note, &note_data);
		if (res) {
			printf("Error!\n");
			retval = 1;
			continue;
		}

		if (!note_data.valid) {
			printf("Free\n");
			continue;
		}

		format_note_name(note_data.utf8_name, name, sizeof(name));
		printf("%-20s (%3d blocks)", name, note_data.blocks);
		if (verbose) {
			/* Header bytes 0-3: game code (last byte is the region),
			 * bytes 4-5: publisher code. Usually printable ASCII. */
			printf("  game ");
			print_code(note_data.raw_data, 4);
			printf("  publisher ");
			print_code(note_data.raw_data + 4, 2);
		}
		if (strlen(mpk->note_comments[note]) > 0) {
			printf("  { %s }", mpk->note_comments[note]);
		}
		printf("\n");
	}

	mempak_free(mpk);
	return retval;
}

/********************************* rm *********************************/

static void usage_rm(void)
{
	printf("Usage: mempak rm [options] PAKFILE NOTE\n");
	printf("\n");
	printf("Delete note NOTE (0-15, as shown by 'mempak ls') from a mempak image.\n");
	printf("The image file is modified in place.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -h, --help      Show this help\n");
}

int cmd_rm(int argc, char **argv)
{
	int help = 0, noteid;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos;
	mempak_structure_t *mpk;
	entry_structure_t entry;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_rm();
		return 0;
	}
	if (n_pos != 2) {
		usage_rm();
		return 2;
	}
	if (cli_parse_note(pos[1], &noteid))
		return 2;

	mpk = mempak_loadFromFile(pos[0]);
	if (!mpk) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}

	if (0 != get_mempak_entry(mpk, noteid, &entry)) {
		fprintf(stderr, "Could not get note %d\n", noteid);
		mempak_free(mpk);
		return 1;
	}

	if (!entry.valid) {
		fprintf(stderr, "Note %d is already free\n", noteid);
		mempak_free(mpk);
		return 1;
	}

	printf("Deleting note %d: %s (%d blocks)\n", noteid, entry.utf8_name, entry.blocks);

	if (0 != delete_mempak_entry(mpk, &entry)) {
		fprintf(stderr, "Error deleting note\n");
		mempak_free(mpk);
		return 1;
	}
	/* A deleted note no longer owns its comment. */
	mpk->note_comments[noteid][0] = 0;

	if (0 != mempak_saveToFile(mpk, pos[0], mpk->file_format)) {
		fprintf(stderr, "Could not write mempak file '%s'\n", pos[0]);
		mempak_free(mpk);
		return 1;
	}

	mempak_free(mpk);
	return 0;
}

/****************************** extract *******************************/

static void usage_extract(void)
{
	printf("Usage: mempak extract [options] PAKFILE NOTE OUTFILE\n");
	printf("\n");
	printf("Export note NOTE (0-15, as shown by 'mempak ls') to a standalone note file.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -h, --help      Show this help\n");
}

int cmd_extract(int argc, char **argv)
{
	int help = 0, noteid;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos;
	mempak_structure_t *mpk;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_extract();
		return 0;
	}
	if (n_pos != 3) {
		usage_extract();
		return 2;
	}
	if (cli_parse_note(pos[1], &noteid))
		return 2;

	mpk = mempak_loadFromFile(pos[0]);
	if (!mpk) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}

	if (mempak_exportNote(mpk, noteid, pos[2])) {
		fprintf(stderr, "Could not export note %d\n", noteid);
		mempak_free(mpk);
		return 1;
	}

	printf("Exported note %d to '%s'\n", noteid, pos[2]);

	mempak_free(mpk);
	return 0;
}

/******************************* insert *******************************/

static void usage_insert(void)
{
	printf("Usage: mempak insert [options] PAKFILE NOTEFILE\n");
	printf("\n");
	printf("Import a note file into a mempak image. The image file is modified in place.\n");
	printf("Without -d or -s the note goes to the first free slot.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -s, --slot N        Write to note slot N (0-15), empty or not\n");
	printf("  -d, --dst N         Overwrite note slot N (0-15); the slot must not be empty\n");
	printf("  -c, --comment TEXT  Note comment (only stored in .n64 images)\n");
	printf("  -h, --help          Show this help\n");
}

int cmd_insert(int argc, char **argv)
{
	int help = 0;
	const char *dst_str = NULL, *slot_str = NULL, *comment = NULL;
	int dst_id = -1, slot_id = -1, used_note_id = -1;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos;
	mempak_structure_t *mpk;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
		{ 'd', "dst", &dst_str, NULL },
		{ 's', "slot", &slot_str, NULL },
		{ 'c', "comment", &comment, NULL },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_insert();
		return 0;
	}
	if (n_pos != 2) {
		usage_insert();
		return 2;
	}
	if (dst_str && slot_str) {
		fprintf(stderr, "Use either -d or -s, not both.\n");
		return 2;
	}
	if (dst_str && cli_parse_note(dst_str, &dst_id))
		return 2;
	if (slot_str && cli_parse_note(slot_str, &slot_id))
		return 2;
	if (comment && strlen(comment) > (MAX_NOTE_COMMENT_SIZE-2)) {
		fprintf(stderr, "Comment too long (%d characters max.)\n", (MAX_NOTE_COMMENT_SIZE-2));
		return 2;
	}

	mpk = mempak_loadFromFile(pos[0]);
	if (!mpk) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}

	if (mempak_importNoteEx(mpk, pos[1], slot_id >= 0 ? slot_id : dst_id, slot_id < 0, &used_note_id)) {
		fprintf(stderr, "Could not import note '%s'\n", pos[1]);
		mempak_free(mpk);
		return 1;
	}

	printf("Note imported into slot %d\n", used_note_id);

	/* The slot may previously have held another note's comment. */
	mpk->note_comments[used_note_id][0] = 0;
	if (comment) {
		if (mpk->file_format != MPK_FORMAT_N64) {
			fprintf(stderr, "Warning: Ignoring comment since it cannot be stored in %s format. Use the n64 format instead.\n",
				mempak_format2string(mpk->file_format));
		} else {
			strncpy(mpk->note_comments[used_note_id], comment, MAX_NOTE_COMMENT_SIZE - 1);
			mpk->note_comments[used_note_id][MAX_NOTE_COMMENT_SIZE - 1] = 0;
		}
	}

	if (0 != mempak_saveToFile(mpk, pos[0], mpk->file_format)) {
		fprintf(stderr, "Could not write mempak file '%s'\n", pos[0]);
		mempak_free(mpk);
		return 1;
	}

	mempak_free(mpk);
	return 0;
}

/******************************* format *******************************/

static void usage_format(void)
{
	printf("Usage: mempak format [options] OUTFILE\n");
	printf("\n");
	printf("Create a new, empty (formatted) mempak image file.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -f, --format FMT  Output format (default: from extension, else %s)\n", DEFAULT_FORMAT_STR);
	printf("  -h, --help        Show this help\n");
	printf("\n");
	cli_print_formats();
}

int cmd_format(int argc, char **argv)
{
	int help = 0, fmt;
	const char *format = NULL;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos;
	mempak_structure_t *mpk;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
		{ 'f', "format", &format, NULL },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_format();
		return 0;
	}
	if (n_pos != 1) {
		usage_format();
		return 2;
	}

	fmt = cli_resolve_format(format, pos[0]);
	if (fmt == MPK_FORMAT_INVALID)
		return 2;

	mpk = mempak_new();
	if (!mpk)
		return 1;

	if (0 != mempak_saveToFile(mpk, pos[0], fmt)) {
		fprintf(stderr, "Could not write mempak file '%s'\n", pos[0]);
		mempak_free(mpk);
		return 1;
	}
	mempak_free(mpk);

	printf("Wrote empty mempak image '%s' (%s format)\n", pos[0], mempak_format2string(fmt));
	return 0;
}

/****************************** convert *******************************/

static void usage_convert(void)
{
	printf("Usage: mempak convert [options] INFILE OUTFILE\n");
	printf("\n");
	printf("Convert a mempak image between formats.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -f, --format FMT  Output format (default: from extension, else %s)\n", DEFAULT_FORMAT_STR);
	printf("  -h, --help        Show this help\n");
	printf("\n");
	cli_print_formats();
}

int cmd_convert(int argc, char **argv)
{
	int help = 0, fmt;
	const char *format = NULL;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos;
	mempak_structure_t *mpk;
	const cli_opt_t opts[] = {
		{ 'h', "help", NULL, &help },
		{ 'f', "format", &format, NULL },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (help) {
		usage_convert();
		return 0;
	}
	if (n_pos != 2) {
		usage_convert();
		return 2;
	}

	fmt = cli_resolve_format(format, pos[1]);
	if (fmt == MPK_FORMAT_INVALID)
		return 2;

	mpk = mempak_loadFromFile(pos[0]);
	if (!mpk) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}

	if (0 != mempak_saveToFile(mpk, pos[1], fmt)) {
		fprintf(stderr, "Could not write mempak file '%s'\n", pos[1]);
		mempak_free(mpk);
		return 1;
	}

	printf("Converted '%s' (%s) to '%s' (%s)\n", pos[0], mempak_format2string(mpk->file_format),
		pos[1], mempak_format2string(fmt));
	mempak_free(mpk);
	return 0;
}
