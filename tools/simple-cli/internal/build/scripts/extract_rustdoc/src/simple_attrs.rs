//! WHAT A MEMBER SAYS ABOUT THE VALUES IT TAKES, READ FROM `#[simple(…)]`.
//!
//! The Rust SDK's `#[derive(Schema)]` makes this attribute legal on a member
//! and checks it when the action is compiled. It generates nothing: the limits
//! an author writes there reach a caller only if this program reads them and
//! puts them in the schema. The forms read here are the ones the SDK documents.
//!
//! | written                    | read as                             |
//! | -------------------------- | ----------------------------------- |
//! | `range(min = …, max = …)`  | either bound, any number            |
//! | `length(min = …, max = …)` | either bound, a whole number from 0 |
//! | `pattern = "…"`            | a string                            |
//! | `format = "…"`             | a string                            |
//! | `default = …`              | a string, a number or a boolean     |
//! | `example = …`              | a string, a number or a boolean     |
//! | `deprecated`               | written on its own                  |
//!
//! Which schema keyword each one becomes depends on the member's type, so that
//! is decided where the type is known, in `schema.rs`.
//!
//! ANYTHING ELSE IS LEFT OUT AND REPORTED, AND NEVER REFUSED. A form this
//! program does not read may be one the SDK accepts after this was written, and
//! a build that passes must keep passing when the SDK moves ahead of it. A form
//! today's SDK refuses already stops the action's own compile, at the line that
//! holds it. Either way the schema says less than the author wrote, and the
//! report says what.

use serde_json::{Number, Value};
use syn::punctuated::Punctuated;
use syn::{
    Attribute, Expr, ExprLit, ExprUnary, Lit, Meta, MetaList, MetaNameValue, Path, Token, UnOp,
};

/// One constraint a member writes.
#[derive(Debug, Clone, PartialEq)]
pub enum Constraint {
    Range(Bounds),
    Length(Bounds),
    Pattern(String),
    Format(String),
    Default(Value),
    Example(Value),
    Deprecated,
    /// Written and not read: the word it was written with, and why.
    Unread(String, &'static str),
}

impl Constraint {
    /// The word an author wrote it with.
    pub fn name(&self) -> &str {
        match self {
            Self::Range(_) => "range",
            Self::Length(_) => "length",
            Self::Pattern(_) => "pattern",
            Self::Format(_) => "format",
            Self::Default(_) => "default",
            Self::Example(_) => "example",
            Self::Deprecated => "deprecated",
            Self::Unread(written, _) => written,
        }
    }
}

/// The two ends of a `range` or a `length`. Either may be written alone.
#[derive(Debug, Default, Clone, PartialEq)]
pub struct Bounds {
    pub min: Option<Number>,
    pub max: Option<Number>,
}

/// Every constraint a member writes, in the order it writes them, whether they
/// share one attribute or have one each.
pub fn constraints_of(attrs: &[Attribute]) -> Vec<Constraint> {
    let mut written: Vec<Constraint> = Vec::new();

    for attr in attrs {
        // `cfg_attr` applies what it holds in some builds only, and which ones
        // cannot be told from here.
        if attr.path().is_ident("cfg_attr") && holds_simple(&attr.meta) {
            written.push(Constraint::Unread(
                "cfg_attr(…, simple(…))".to_string(),
                IN_SOME_BUILDS,
            ));
            continue;
        }

        if !attr.path().is_ident("simple") {
            continue;
        }

        let Ok(items) = attr.parse_args_with(Punctuated::<Meta, Token![,]>::parse_terminated)
        else {
            written.push(Constraint::Unread("#[simple]".to_string(), NOT_READ));
            continue;
        };

        for item in &items {
            let read = constraint(item)
                .unwrap_or_else(|| Constraint::Unread(spelled(item.path()), NOT_READ));

            // Written twice, there are two answers. The SDK refuses that, and
            // the first is the one kept here.
            let repeated = !matches!(read, Constraint::Unread(..))
                && written.iter().any(|seen| seen.name() == read.name());

            written.push(match repeated {
                true => Constraint::Unread(read.name().to_string(), WRITTEN_TWICE),
                false => read,
            });
        }
    }

    written
}

const NOT_READ: &str = "it is not in a form this build step reads exactly";
const WRITTEN_TWICE: &str = "it is written a second time, and only the first is read";
const IN_SOME_BUILDS: &str =
    "`cfg_attr` applies it in some builds only, and this build step cannot tell which";

/// Whether a `cfg_attr` holds `simple(…)`, at any depth of `cfg_attr`.
fn holds_simple(meta: &Meta) -> bool {
    let Meta::List(list) = meta else {
        return false;
    };

    let Ok(held) = list.parse_args_with(Punctuated::<Meta, Token![,]>::parse_terminated) else {
        return false;
    };

    // The first item is the condition, and the rest are what it applies.
    held.iter().skip(1).any(|meta| {
        meta.path().is_ident("simple") || (meta.path().is_ident("cfg_attr") && holds_simple(meta))
    })
}

fn constraint(item: &Meta) -> Option<Constraint> {
    match item {
        Meta::Path(path) if path.is_ident("deprecated") => Some(Constraint::Deprecated),
        Meta::List(list) if list.path.is_ident("range") => {
            bounds(list, false).map(Constraint::Range)
        }
        Meta::List(list) if list.path.is_ident("length") => {
            bounds(list, true).map(Constraint::Length)
        }
        Meta::NameValue(pair) if pair.path.is_ident("pattern") => {
            text(&pair.value).map(Constraint::Pattern)
        }
        Meta::NameValue(pair) if pair.path.is_ident("format") => {
            text(&pair.value).map(Constraint::Format)
        }
        Meta::NameValue(pair) if pair.path.is_ident("default") => {
            literal(&pair.value).map(Constraint::Default)
        }
        Meta::NameValue(pair) if pair.path.is_ident("example") => {
            literal(&pair.value).map(Constraint::Example)
        }
        _ => None,
    }
}

/// `(min = …, max = …)`, where `counted` bounds are whole and never below zero
/// because they count characters or elements.
fn bounds(list: &MetaList, counted: bool) -> Option<Bounds> {
    let pairs = list
        .parse_args_with(Punctuated::<MetaNameValue, Token![,]>::parse_terminated)
        .ok()?;
    let mut read = Bounds::default();

    for pair in &pairs {
        let end = if pair.path.is_ident("min") {
            &mut read.min
        } else if pair.path.is_ident("max") {
            &mut read.max
        } else {
            return None;
        };

        if end.is_some() {
            return None;
        }

        *end = Some(number(&pair.value, counted)?);
    }

    Some(read)
}

/// `"…"`, for the two constraints that take a string.
fn text(value: &Expr) -> Option<String> {
    match signed(value)? {
        ("", Lit::Str(written)) => Some(written.value()),
        _ => None,
    }
}

/// A string, a number or a boolean, for the two constraints that take a value
/// of the member's own type.
fn literal(value: &Expr) -> Option<Value> {
    match signed(value)? {
        (_, Lit::Int(_) | Lit::Float(_)) => number(value, false).map(Value::Number),
        ("", Lit::Str(written)) => Some(Value::String(written.value())),
        ("", Lit::Char(written)) => Some(Value::String(written.value().to_string())),
        ("", Lit::Bool(written)) => Some(Value::Bool(written.value)),
        _ => None,
    }
}

/// A number as JSON carries it. A whole one is kept exactly, and is not read
/// once 64 bits cannot hold it: rounded, it would be a limit nobody wrote. A
/// count is whole and never below zero.
fn number(value: &Expr, counted: bool) -> Option<Number> {
    let digits = match signed(value)? {
        (sign, Lit::Int(whole)) => format!("{sign}{}", whole.base10_digits()),
        (sign, Lit::Float(fraction)) if !counted => {
            return Number::from_f64(format!("{sign}{}", fraction.base10_digits()).parse().ok()?);
        }
        _ => return None,
    };

    if counted && digits.starts_with('-') {
        return None;
    }

    digits
        .parse::<i64>()
        .map(Number::from)
        .or_else(|_| digits.parse::<u64>().map(Number::from))
        .ok()
}

/// A literal, and the minus sign written before it if there is one.
fn signed(value: &Expr) -> Option<(&'static str, &Lit)> {
    match value {
        Expr::Lit(ExprLit { lit, attrs }) if attrs.is_empty() => Some(("", lit)),
        Expr::Unary(ExprUnary {
            op: UnOp::Neg(_),
            expr,
            attrs,
        }) if attrs.is_empty() => match expr.as_ref() {
            Expr::Lit(ExprLit { lit, attrs }) if attrs.is_empty() => Some(("-", lit)),
            _ => None,
        },
        _ => None,
    }
}

/// A key as its author spelled it, for the report.
fn spelled(path: &Path) -> String {
    let words: Vec<String> = path
        .segments
        .iter()
        .map(|segment| segment.ident.to_string())
        .collect();

    words.join("::")
}

#[cfg(test)]
mod tests {
    use serde_json::{json, Number};
    use syn::parse_quote;

    use super::{constraints_of, Bounds, Constraint};

    fn read(field: syn::Field) -> Vec<Constraint> {
        constraints_of(&field.attrs)
    }

    #[test]
    fn reads_every_constraint_in_the_order_it_is_written() {
        let written = read(parse_quote! {
            #[simple(range(min = -1.5, max = 90), length(max = 64), pattern = "^KNOW")]
            #[simple(format = "email", default = "a@b.co", example = 7, deprecated)]
            member: String
        });

        assert_eq!(
            written,
            vec![
                Constraint::Range(Bounds {
                    min: json!(-1.5).as_number().cloned(),
                    max: json!(90).as_number().cloned(),
                }),
                Constraint::Length(Bounds {
                    min: None,
                    max: json!(64).as_number().cloned(),
                }),
                Constraint::Pattern("^KNOW".to_string()),
                Constraint::Format("email".to_string()),
                Constraint::Default(json!("a@b.co")),
                Constraint::Example(json!(7)),
                Constraint::Deprecated,
            ]
        );
    }

    #[test]
    fn a_member_that_writes_none_has_none() {
        assert!(read(parse_quote! { #[serde(default)] member: String }).is_empty());
    }

    #[test]
    fn a_number_keeps_the_value_its_author_wrote() {
        let written = read(parse_quote! {
            #[simple(range(min = 0x10, max = 1_000u32), default = 2., example = 1e3)]
            member: f64
        });

        assert_eq!(
            written,
            vec![
                Constraint::Range(Bounds {
                    min: json!(16).as_number().cloned(),
                    max: json!(1000).as_number().cloned(),
                }),
                Constraint::Default(json!(2.0)),
                Constraint::Example(json!(1000.0)),
            ]
        );
    }

    /// JSON carries a whole number exactly while 64 bits hold it. One beyond
    /// that would be rounded into a different limit, so it is not read.
    #[test]
    fn a_whole_number_is_kept_exactly_or_not_read() {
        let written = read(parse_quote! {
            #[simple(range(max = 10000000000000000001))]
            #[simple(length(max = 340282366920938463463374607431768211455))]
            member: u64
        });

        assert_eq!(
            written[0],
            Constraint::Range(Bounds {
                min: None,
                max: Some(Number::from(10_000_000_000_000_000_001_u64)),
            })
        );
        assert!(matches!(written[1], Constraint::Unread(..)), "{written:?}");
    }

    #[test]
    fn a_form_that_is_not_read_keeps_the_word_it_was_written_with() {
        let written = read(parse_quote! {
            #[simple(multiple_of = 5, checks::range(min = 1), pattern = "^A")]
            #[simple(pattern = "^B")]
            member: String
        });

        let names: Vec<&str> = written.iter().map(Constraint::name).collect();

        assert_eq!(
            names,
            vec!["multiple_of", "checks::range", "pattern", "pattern"]
        );
        assert!(matches!(written[3], Constraint::Unread(..)));
    }
}
