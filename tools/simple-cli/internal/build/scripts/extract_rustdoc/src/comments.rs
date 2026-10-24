//! EVERY COMMENT IN THE FILE, READ ONCE, WHATEVER IT IS ATTACHED TO.
//!
//! The exposure statement is heard because it was written, not because of where
//! it was written. That rule cannot be served by the syntax tree here: `syn`
//! sees a doc comment, because a doc comment is an attribute, and it does not
//! see an ordinary `//` line at all — the lexer throws those away before a tree
//! exists. A reader built on the tree would therefore hear the same four lines
//! or not depending on which comment syntax their author reached for, which is
//! the failure the other two generators already fixed once each.
//!
//! So the source text is scanned for comments directly. This is a lexer and not
//! a parser: it decides only where a comment starts and ends, and it must get
//! the three things wrong about Rust that a naive scan gets wrong — block
//! comments NEST, raw strings suspend every escape, and `'` opens a character
//! literal in some places and a lifetime in others.
//!
//! `#[doc = "..."]` IS A COMMENT HERE, because it is one in Rust. `///` is
//! sugar for it, the two are the same attribute by the time anything compiles,
//! and the description this program answers with is read through the tree, which
//! sees both. A scan that read only the slashes would therefore hand back a
//! description containing a line it did not hand back as a comment — and the
//! caller, which refuses tags where they are written, would have nothing to
//! refuse. What that costs is not a missing refusal but the artifact: a retired
//! `@effects` ships into the description as prose a model reads, and an author
//! who wrote `@tool` this way has the line taken out of their prose and gets no
//! `ai` — the action quietly stops being callable, from a run that exits zero.
//! So a doc attribute is read here too, and only in the form the source writes
//! it, so a `///` is not counted twice as itself.
//!
//! Nothing here interprets what a comment says. That is the vocabulary's job,
//! one module over.

/// One comment's text, with the syntax that makes it a comment taken off and
/// nothing else touched: the leading slashes, the `!` of an inner doc comment,
/// the enclosing `/* */`, the `*` that conventionally starts each line of a
/// block comment, and the `#[doc = "..."]` around a doc attribute.
///
/// A tag reads the same whichever syntax carries it, which is the whole point:
/// `///`, `//`, `/** */`, `/* */` and `#[doc = "..."]` are five ways to write
/// the same line, and an author choosing between them is not making a statement
/// about exposure.
pub fn comments_in(source: &str) -> Vec<String> {
    Scanner::new(source).collect()
}

struct Scanner {
    chars: Vec<char>,
    index: usize,
}

impl Scanner {
    fn new(source: &str) -> Self {
        Self {
            chars: source.chars().collect(),
            index: 0,
        }
    }

    fn collect(mut self) -> Vec<String> {
        let mut comments = Vec::new();

        while self.index < self.chars.len() {
            match self.peek(0) {
                Some('/') if self.peek(1) == Some('/') => comments.push(self.line_comment()),
                Some('/') if self.peek(1) == Some('*') => comments.push(self.block_comment()),
                Some('#') => match self.doc_attribute() {
                    Some(text) => comments.push(text),
                    None => self.index += 1,
                },
                Some('"') => self.string_literal(),
                Some('\'') => self.char_literal_or_lifetime(),
                Some('r') | Some('b') if self.raw_string_hashes().is_some() => self.raw_string(),
                _ => self.index += 1,
            }
        }

        comments
    }

    fn peek(&self, ahead: usize) -> Option<char> {
        self.chars.get(self.index + ahead).copied()
    }

    fn line_comment(&mut self) -> String {
        let start = self.index;

        while self.index < self.chars.len() && self.peek(0) != Some('\n') {
            self.index += 1;
        }

        let text: String = self.chars[start..self.index].iter().collect();

        without_line_markers(&text)
    }

    /// A block comment, up to the `*/` that closes the one that was opened.
    ///
    /// Rust nests them, so the depth is counted rather than the first `*/`
    /// taken. Stopping at the first would end the comment inside itself and
    /// hand the rest of the file to the scanner as code — where a stray quote
    /// would then open a string that swallows every comment after it.
    fn block_comment(&mut self) -> String {
        let start = self.index;
        let mut depth = 0usize;

        while self.index < self.chars.len() {
            if self.peek(0) == Some('/') && self.peek(1) == Some('*') {
                depth += 1;
                self.index += 2;
                continue;
            }

            if self.peek(0) == Some('*') && self.peek(1) == Some('/') {
                depth -= 1;
                self.index += 2;

                if depth == 0 {
                    break;
                }

                continue;
            }

            self.index += 1;
        }

        let text: String = self.chars[start..self.index].iter().collect();

        without_block_markers(&text)
    }

    /// The text of a `#[doc = "..."]` attribute starting at the cursor, if one
    /// starts there at all.
    ///
    /// The cursor does not move unless a whole one was read. A `#` begins a
    /// great many things that are not this — `#[derive(…)]`, `#[serde(…)]`, a
    /// raw string's hashes — and a half-consumed attempt would leave the scan
    /// inside a token, where the next quote opens a string that swallows every
    /// comment after it.
    fn doc_attribute(&mut self) -> Option<String> {
        let start = self.index;

        match self.read_doc_attribute() {
            Some(text) => Some(text),
            None => {
                self.index = start;

                None
            }
        }
    }

    fn read_doc_attribute(&mut self) -> Option<String> {
        self.expect('#')?;

        // `#![doc = "..."]` documents the file it is written in, the same way
        // `//!` does.
        if self.peek(0) == Some('!') {
            self.index += 1;
        }

        self.expect('[')?;
        self.skip_whitespace();
        // `#[doc_alias = "…"]` and `#[docs = "…"]` both begin with these three
        // letters and neither is a doc comment, so the `=` has to follow.
        self.expect_word("doc")?;
        self.skip_whitespace();
        self.expect('=')?;
        self.skip_whitespace();

        let text = self.string_literal_value()?;

        self.skip_whitespace();
        self.expect(']')?;

        Some(text)
    }

    fn expect(&mut self, wanted: char) -> Option<()> {
        if self.peek(0) != Some(wanted) {
            return None;
        }

        self.index += 1;

        Some(())
    }

    fn expect_word(&mut self, word: &str) -> Option<()> {
        for (offset, wanted) in word.chars().enumerate() {
            if self.peek(offset) != Some(wanted) {
                return None;
            }
        }

        self.index += word.chars().count();

        Some(())
    }

    fn skip_whitespace(&mut self) {
        while self.peek(0).is_some_and(char::is_whitespace) {
            self.index += 1;
        }
    }

    /// The text a string literal holds, with the escapes resolved.
    ///
    /// Resolved rather than carried through, because a doc attribute writes its
    /// line breaks as `\n` where a `///` comment writes them as line breaks. A
    /// reader that left them escaped would hand the caller one long line, and a
    /// tag is read at the start of a line — so every annotation after the first
    /// would be invisible for no reason its author could see.
    fn string_literal_value(&mut self) -> Option<String> {
        // A BYTE STRING IS NOT A DOC COMMENT. `#[doc = b"…"]` does not compile,
        // and the reader that supplies the description passes over it, so
        // reading a tag out of one here would refuse a line that no description
        // could ever carry — the same two-readers-disagreeing this is fixing,
        // pointed the other way.
        if self.peek(0) == Some('b') {
            return None;
        }

        if let Some(hashes) = self.raw_string_hashes() {
            return self.raw_string_value(hashes);
        }

        self.expect('"')?;

        let mut text = String::new();

        while let Some(character) = self.peek(0) {
            match character {
                '"' => {
                    self.index += 1;

                    return Some(text);
                }
                '\\' => {
                    self.index += 1;
                    self.escape(&mut text)?;
                }
                _ => {
                    text.push(character);
                    self.index += 1;
                }
            }
        }

        None
    }

    /// A raw string's text, which is every character up to the close exactly as
    /// written.
    fn raw_string_value(&mut self, hashes: usize) -> Option<String> {
        if self.peek(0) == Some('b') {
            self.index += 1;
        }

        self.expect('r')?;
        self.index += hashes;
        self.expect('"')?;

        let start = self.index;

        while self.index < self.chars.len() {
            if self.peek(0) == Some('"')
                && (1..=hashes).all(|offset| self.peek(offset) == Some('#'))
            {
                let text: String = self.chars[start..self.index].iter().collect();

                self.index += hashes + 1;

                return Some(text);
            }

            self.index += 1;
        }

        None
    }

    /// One escape sequence, with the cursor sitting just after the backslash.
    ///
    /// Every escape Rust defines for a string, because an author writing
    /// `\u{40}tool` has written the same line as one writing `@tool` and the
    /// compiler cannot tell them apart.
    fn escape(&mut self, text: &mut String) -> Option<()> {
        let character = self.peek(0)?;

        self.index += 1;

        match character {
            'n' => text.push('\n'),
            'r' => text.push('\r'),
            't' => text.push('\t'),
            '0' => text.push('\0'),
            '\\' | '"' | '\'' => text.push(character),
            'x' => {
                let value = self.hex_digits(2)?;

                text.push(char::from_u32(value)?);
            }
            'u' => {
                self.expect('{')?;

                let mut value = 0u32;

                while let Some(digit) = self.peek(0) {
                    // `\u{1_F600}` is one character, and the underscore is a
                    // separator rather than part of the number.
                    if digit == '_' {
                        self.index += 1;

                        continue;
                    }

                    let Some(digit) = digit.to_digit(16) else {
                        break;
                    };

                    value = value.checked_mul(16)?.checked_add(digit)?;
                    self.index += 1;
                }

                self.expect('}')?;
                text.push(char::from_u32(value)?);
            }
            // A backslash at the end of a line continues it, and the
            // indentation of the line below is layout rather than text.
            '\n' => self.skip_whitespace(),
            other => text.push(other),
        }

        Some(())
    }

    fn hex_digits(&mut self, count: usize) -> Option<u32> {
        let mut value = 0u32;

        for _ in 0..count {
            let digit = self.peek(0)?.to_digit(16)?;

            value = value * 16 + digit;
            self.index += 1;
        }

        Some(value)
    }

    fn string_literal(&mut self) {
        self.index += 1;

        while self.index < self.chars.len() {
            match self.peek(0) {
                Some('\\') => self.index += 2,
                Some('"') => {
                    self.index += 1;
                    return;
                }
                _ => self.index += 1,
            }
        }
    }

    /// The number of `#` in a raw string prefix at the cursor, if a raw string
    /// starts here at all.
    ///
    /// `r`, `br`, `r#`, `br##"` and so on. A bare `r` that is part of an
    /// identifier is not one, so the prefix only counts when a quote closes it.
    fn raw_string_hashes(&self) -> Option<usize> {
        let mut offset = 0;

        if self.peek(offset) == Some('b') {
            offset += 1;
        }

        if self.peek(offset) != Some('r') {
            return None;
        }

        // An identifier character before the prefix makes this the tail of a
        // longer word rather than a literal.
        if self.index > 0 {
            let previous = self.chars[self.index - 1];

            if previous.is_alphanumeric() || previous == '_' {
                return None;
            }
        }

        offset += 1;
        let mut hashes = 0;

        while self.peek(offset) == Some('#') {
            hashes += 1;
            offset += 1;
        }

        if self.peek(offset) == Some('"') {
            Some(hashes)
        } else {
            None
        }
    }

    fn raw_string(&mut self) {
        let hashes = match self.raw_string_hashes() {
            Some(hashes) => hashes,
            None => {
                self.index += 1;
                return;
            }
        };

        while self.peek(0) != Some('"') {
            self.index += 1;
        }

        self.index += 1;

        while self.index < self.chars.len() {
            if self.peek(0) == Some('"') {
                let closed = (1..=hashes).all(|offset| self.peek(offset) == Some('#'));

                if closed {
                    self.index += hashes + 1;
                    return;
                }
            }

            self.index += 1;
        }
    }

    /// A `'` opens a character literal or names a lifetime, and the two are told
    /// apart by what follows rather than assumed.
    ///
    /// Reading `'static` as an unterminated literal would run to the next `'` in
    /// the file and eat every comment in between; reading `'"'` as a lifetime
    /// would leave a quote open and do the same.
    fn char_literal_or_lifetime(&mut self) {
        if self.peek(1) == Some('\\') {
            self.index += 2;

            while self.index < self.chars.len() {
                self.index += 1;

                if self.peek(0) == Some('\'') {
                    self.index += 1;
                    return;
                }
            }

            return;
        }

        if self.peek(2) == Some('\'') {
            self.index += 3;
            return;
        }

        // A lifetime: only the quote is consumed, and the name after it is
        // ordinary code.
        self.index += 1;
    }
}

fn without_line_markers(comment: &str) -> String {
    let text = comment.trim_start_matches('/');

    text.strip_prefix('!').unwrap_or(text).to_string()
}

fn without_block_markers(comment: &str) -> String {
    let inner = comment
        .trim_start_matches('/')
        .trim_start_matches('*')
        .trim_end_matches('/')
        .trim_end_matches('*');

    inner
        .lines()
        .map(|line| {
            let trimmed = line.trim_start();

            match trimmed.strip_prefix('*') {
                Some(rest) => rest.strip_prefix(' ').unwrap_or(rest),
                None => line,
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}

#[cfg(test)]
mod tests {
    use super::comments_in;

    #[test]
    fn reads_line_doc_and_ordinary_comments_alike() {
        let comments = comments_in("/// doc\n// plain\n//! inner\nfn main() {}\n");

        assert_eq!(comments, vec![" doc", " plain", " inner"]);
    }

    #[test]
    fn reads_block_comments_and_strips_the_leading_star() {
        let comments = comments_in("/**\n * @tool\n * line\n */\nfn main() {}\n");

        assert_eq!(comments.len(), 1);
        assert_eq!(
            comments[0].lines().map(str::trim).collect::<Vec<_>>(),
            vec!["", "@tool", "line", ""]
        );
    }

    #[test]
    fn counts_nested_block_comments_rather_than_stopping_at_the_first_close() {
        let comments = comments_in("/* outer /* inner */ still outer */\n// after\n");

        assert_eq!(comments.len(), 2);
        assert!(comments[0].contains("still outer"));
        assert_eq!(comments[1], " after");
    }

    #[test]
    fn ignores_comment_syntax_inside_string_and_raw_string_literals() {
        let source = "let a = \"// not a comment\";\nlet b = r#\"/* nor this */\"#;\n// real\n";

        assert_eq!(comments_in(source), vec![" real"]);
    }

    #[test]
    fn tells_a_lifetime_apart_from_a_character_literal() {
        let source = "struct A<'a> { q: &'a str }\nlet slash = '/';\nlet quote = '\"';\n// real\n";

        assert_eq!(comments_in(source), vec![" real"]);
    }

    #[test]
    fn reads_an_escaped_quote_character_literal_without_opening_a_string() {
        assert_eq!(comments_in("let c = '\\'';\n// real\n"), vec![" real"]);
    }

    #[test]
    fn reads_a_doc_attribute_because_rust_reads_it_as_a_doc_comment() {
        // The tree-based reader that supplies the description sees this and a
        // `///` as the same attribute. A scan that saw only the slashes would
        // put a line in the description that never reached the caller's
        // vocabulary, and a retired tag written this way would ship as prose.
        assert_eq!(
            comments_in("#[doc = \"@effects write\"]\nstruct Payload {}\n"),
            vec!["@effects write"]
        );
    }

    #[test]
    fn reads_an_inner_doc_attribute() {
        assert_eq!(comments_in("#![doc = \"@tool\"]\n"), vec!["@tool"]);
    }

    #[test]
    fn resolves_the_escapes_so_every_line_of_a_doc_attribute_is_a_line() {
        // One attribute may carry a whole comment, and a tag is read at the
        // start of a line. Left escaped, this is one line and only the first
        // annotation would ever be seen.
        assert_eq!(
            comments_in("#[doc = \"Totals.\\n@tool\\n@short_desc Totals.\"]\n"),
            vec!["Totals.\n@tool\n@short_desc Totals."]
        );
    }

    #[test]
    fn resolves_an_escape_that_spells_the_start_of_an_annotation() {
        assert_eq!(
            comments_in("#[doc = \"\\u{40}effects write\"]\n"),
            vec!["@effects write"]
        );
        assert_eq!(
            comments_in("#[doc = \"\\x40retry safe\"]\n"),
            vec!["@retry safe"]
        );
    }

    #[test]
    fn reads_a_raw_string_doc_attribute_exactly_as_written() {
        assert_eq!(
            comments_in("#[doc = r\"@effects write\"]\n"),
            vec!["@effects write"]
        );
        assert_eq!(
            comments_in("#[doc = r#\"@discloses \"tenant\"\"#]\n"),
            vec!["@discloses \"tenant\""]
        );
    }

    #[test]
    fn reads_a_doc_attribute_written_across_lines() {
        assert_eq!(comments_in("#[ doc\n  =\n  \"@tool\" ]\n"), vec!["@tool"]);
    }

    #[test]
    fn a_doc_comment_is_not_counted_a_second_time_as_the_attribute_it_desugars_to() {
        // `///` IS `#[doc = "…"]` once anything compiles, but the source says
        // `///`. Counting it as both is a second `@tool` in a file that writes
        // one, and the caller refuses a tag declared twice.
        assert_eq!(
            comments_in("/// @tool\nstruct Payload {}\n"),
            vec![" @tool"]
        );
    }

    #[test]
    fn an_attribute_that_is_not_a_doc_comment_is_not_read_as_one() {
        let source = "#[derive(Deserialize)]\n#[serde(rename_all = \"camelCase\")]\n\
                      #[doc_alias = \"invoice\"]\n#[docs = \"@tool\"]\n#[doc(hidden)]\n// real\n";

        assert_eq!(comments_in(source), vec![" real"]);
    }

    #[test]
    fn a_doc_attribute_written_inside_a_string_or_a_comment_is_not_one() {
        let source = "let a = \"#[doc = \\\"@tool\\\"]\";\n\
                      // #[doc = \"@effects write\"]\n\
                      let b = r#\"#[doc = \"@retry safe\"]\"#;\n";
        let comments = comments_in(source);

        assert_eq!(comments.len(), 1);
        assert!(comments[0].starts_with(" #[doc"), "{comments:?}");
    }

    #[test]
    fn a_byte_string_is_not_a_doc_comment() {
        // It does not compile, and the description is read by something that
        // passes over it. Reading a tag here would refuse a line no description
        // could carry.
        let comments = comments_in("#[doc = b\"@tool\"]\n#[doc = br\"@effects write\"]\n// real\n");

        assert_eq!(comments, vec![" real"]);
    }

    #[test]
    fn an_unfinished_attribute_leaves_the_scan_where_it_found_it() {
        // The cursor must not be left inside a token by a failed attempt: the
        // quote below would then open a string and eat the comment after it.
        assert_eq!(
            comments_in("#[doc = 1]\nlet a = \"x\";\n// real\n"),
            vec![" real"]
        );
        assert_eq!(comments_in("#[cfg(test)]\n// real\n"), vec![" real"]);
    }
}
