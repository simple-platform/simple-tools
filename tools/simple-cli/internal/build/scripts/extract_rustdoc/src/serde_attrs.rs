//! WHAT `serde` WILL ACTUALLY ACCEPT, READ FROM THE ATTRIBUTES THAT DECIDE IT.
//!
//! A Rust action's payload is deserialized by `serde`, so the schema an agent
//! is handed has to describe what `serde` accepts and not what the struct looks
//! like. The two differ wherever an attribute is written: `rename` changes a
//! property's name, `rename_all` changes every one of them, `skip` removes a
//! property outright, and `default` makes a required one optional.
//!
//! REQUIREDNESS IS THE TYPE, plus these attributes. There is no tag for an
//! author to write and none to get wrong — a member is optional because it is
//! an `Option` or because it has a default, and required otherwise.

use syn::meta::ParseNestedMeta;
use syn::{Attribute, LitStr, Token};

use crate::tags::{refuse, Refusal};

#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ContainerAttrs {
    pub rename_all: Option<RenameRule>,
    /// `#[serde(default)]` on the container: every member becomes optional.
    pub default: bool,
}

#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct MemberAttrs {
    pub rename: Option<String>,
    pub default: bool,
    pub skip: bool,
    pub flatten: bool,
}

/// The `rename_all` styles `serde` ships, applied the way `serde` applies them.
///
/// The rule reads a FIELD name as `snake_case` and a VARIANT name as
/// `PascalCase`, because that is what the language's own conventions produce,
/// and it renames accordingly. Reimplementing it as one generic word-splitter
/// would agree with `serde` on the common names and quietly disagree on the
/// rest — and a property name the schema and the deserializer disagree about is
/// a call the model gets right and the action rejects.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RenameRule {
    Lower,
    Upper,
    Pascal,
    Camel,
    Snake,
    ScreamingSnake,
    Kebab,
    ScreamingKebab,
}

impl RenameRule {
    fn from_style(raw: &str) -> Option<Self> {
        match raw {
            "lowercase" => Some(Self::Lower),
            "UPPERCASE" => Some(Self::Upper),
            "PascalCase" => Some(Self::Pascal),
            "camelCase" => Some(Self::Camel),
            "snake_case" => Some(Self::Snake),
            "SCREAMING_SNAKE_CASE" => Some(Self::ScreamingSnake),
            "kebab-case" => Some(Self::Kebab),
            "SCREAMING-KEBAB-CASE" => Some(Self::ScreamingKebab),
            _ => None,
        }
    }

    pub fn accepted() -> Vec<String> {
        [
            "lowercase",
            "UPPERCASE",
            "PascalCase",
            "camelCase",
            "snake_case",
            "SCREAMING_SNAKE_CASE",
            "kebab-case",
            "SCREAMING-KEBAB-CASE",
        ]
        .iter()
        .map(|style| (*style).to_string())
        .collect()
    }

    /// A field name, which the rule reads as `snake_case`.
    pub fn apply_to_field(self, field: &str) -> String {
        match self {
            Self::Lower | Self::Snake => field.to_string(),
            Self::Upper | Self::ScreamingSnake => field.to_uppercase(),
            Self::Pascal => pascal_from_snake(field),
            Self::Camel => {
                let pascal = pascal_from_snake(field);
                lower_first(&pascal)
            }
            Self::Kebab => field.replace('_', "-"),
            Self::ScreamingKebab => field.to_uppercase().replace('_', "-"),
        }
    }

    /// A variant name, which the rule reads as `PascalCase`.
    pub fn apply_to_variant(self, variant: &str) -> String {
        match self {
            Self::Lower => variant.to_lowercase(),
            Self::Upper => variant.to_uppercase(),
            Self::Pascal => variant.to_string(),
            Self::Camel => lower_first(variant),
            Self::Snake => snake_from_pascal(variant),
            Self::ScreamingSnake => snake_from_pascal(variant).to_uppercase(),
            Self::Kebab => snake_from_pascal(variant).replace('_', "-"),
            Self::ScreamingKebab => snake_from_pascal(variant).to_uppercase().replace('_', "-"),
        }
    }
}

fn pascal_from_snake(field: &str) -> String {
    let mut out = String::new();
    let mut capitalize = true;

    for character in field.chars() {
        if character == '_' {
            capitalize = true;
            continue;
        }

        if capitalize {
            out.extend(character.to_uppercase());
            capitalize = false;
        } else {
            out.push(character);
        }
    }

    out
}

fn snake_from_pascal(variant: &str) -> String {
    let mut out = String::new();

    for (index, character) in variant.chars().enumerate() {
        if character.is_uppercase() {
            if index > 0 {
                out.push('_');
            }

            out.extend(character.to_lowercase());
        } else {
            out.push(character);
        }
    }

    out
}

fn lower_first(name: &str) -> String {
    let mut characters = name.chars();

    match characters.next() {
        Some(first) => first.to_lowercase().chain(characters).collect(),
        None => String::new(),
    }
}

pub fn container_attrs(action: &str, attrs: &[Attribute]) -> Result<ContainerAttrs, Refusal> {
    let mut read = ContainerAttrs::default();

    for attr in serde_attrs(attrs) {
        let mut rename_all: Option<String> = None;

        parse(action, attr, |meta| {
            if meta.path.is_ident("rename_all") {
                rename_all = renaming(&meta)?;
                return Ok(());
            }

            if meta.path.is_ident("default") {
                read.default = true;
                return skip_value(&meta);
            }

            skip_value(&meta)
        })?;

        if let Some(style) = rename_all {
            read.rename_all = Some(rule(action, &style)?);
        }
    }

    Ok(read)
}

pub fn member_attrs(action: &str, attrs: &[Attribute]) -> Result<MemberAttrs, Refusal> {
    let mut read = MemberAttrs::default();

    for attr in serde_attrs(attrs) {
        parse(action, attr, |meta| {
            if meta.path.is_ident("rename") {
                if let Some(name) = renaming(&meta)? {
                    read.rename = Some(name);
                }

                return Ok(());
            }

            if meta.path.is_ident("default") {
                read.default = true;
                return skip_value(&meta);
            }

            // `skip_serializing` is deliberately absent: it says nothing about
            // what may be SENT to the action, and a member removed from the
            // schema because of it is a member an agent can no longer supply.
            if meta.path.is_ident("skip") || meta.path.is_ident("skip_deserializing") {
                read.skip = true;
                return skip_value(&meta);
            }

            if meta.path.is_ident("flatten") {
                read.flatten = true;
                return skip_value(&meta);
            }

            skip_value(&meta)
        })?;
    }

    Ok(read)
}

fn rule(action: &str, style: &str) -> Result<RenameRule, Refusal> {
    RenameRule::from_style(style).ok_or_else(|| {
        refuse(
            action,
            &format!("writes serde(rename_all = {style:?}), which is not a style serde renames by"),
            &RenameRule::accepted(),
        )
    })
}

fn serde_attrs(attrs: &[Attribute]) -> impl Iterator<Item = &Attribute> {
    attrs.iter().filter(|attr| attr.path().is_ident("serde"))
}

/// A `#[serde(...)]` attribute, walked once.
///
/// A malformed one REFUSES rather than being ignored. Every attribute in here
/// changes the shape of what the action accepts, so one this generator could
/// not read is a schema it cannot vouch for — and a schema nobody can vouch for
/// is exactly what the caller must not write over the last good one.
fn parse(
    action: &str,
    attr: &Attribute,
    logic: impl FnMut(ParseNestedMeta) -> syn::Result<()>,
) -> Result<(), Refusal> {
    attr.parse_nested_meta(logic).map_err(|error| {
        refuse(
            action,
            &format!("writes a serde attribute this generator cannot read: {error}"),
            &[],
        )
    })
}

/// The name an attribute renames to, in either form `serde` accepts it.
///
/// `rename = "x"` states one name for both directions; `rename(deserialize =
/// "x")` states the one that matters here, since a payload is only ever
/// deserialized. A `serialize`-only rename says nothing about what the action
/// accepts and is left alone.
fn renaming(meta: &ParseNestedMeta) -> syn::Result<Option<String>> {
    if meta.input.peek(Token![=]) {
        let literal: LitStr = meta.value()?.parse()?;

        return Ok(Some(literal.value()));
    }

    if meta.input.peek(syn::token::Paren) {
        let mut found = None;

        meta.parse_nested_meta(|inner| {
            if inner.path.is_ident("deserialize") {
                let literal: LitStr = inner.value()?.parse()?;
                found = Some(literal.value());

                return Ok(());
            }

            skip_value(&inner)
        })?;

        return Ok(found);
    }

    Ok(None)
}

/// Whatever follows an attribute this generator does not act on, consumed so
/// the walk reaches the next one.
fn skip_value(meta: &ParseNestedMeta) -> syn::Result<()> {
    if meta.input.peek(Token![=]) {
        let _: syn::Expr = meta.value()?.parse()?;

        return Ok(());
    }

    if meta.input.peek(syn::token::Paren) {
        return meta.parse_nested_meta(|inner| skip_value(&inner));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::RenameRule;

    #[test]
    fn renames_fields_the_way_serde_renames_them() {
        assert_eq!(
            RenameRule::Camel.apply_to_field("customer_id"),
            "customerId"
        );
        assert_eq!(
            RenameRule::Pascal.apply_to_field("customer_id"),
            "CustomerId"
        );
        assert_eq!(
            RenameRule::Kebab.apply_to_field("customer_id"),
            "customer-id"
        );
        assert_eq!(
            RenameRule::Snake.apply_to_field("customer_id"),
            "customer_id"
        );
        assert_eq!(
            RenameRule::Lower.apply_to_field("customer_id"),
            "customer_id"
        );
        assert_eq!(
            RenameRule::Upper.apply_to_field("customer_id"),
            "CUSTOMER_ID"
        );
        assert_eq!(
            RenameRule::ScreamingSnake.apply_to_field("customer_id"),
            "CUSTOMER_ID"
        );
        assert_eq!(
            RenameRule::ScreamingKebab.apply_to_field("customer_id"),
            "CUSTOMER-ID"
        );
    }

    #[test]
    fn renames_variants_the_way_serde_renames_them() {
        assert_eq!(RenameRule::Lower.apply_to_variant("OpenOnly"), "openonly");
        assert_eq!(RenameRule::Snake.apply_to_variant("OpenOnly"), "open_only");
        assert_eq!(RenameRule::Kebab.apply_to_variant("OpenOnly"), "open-only");
        assert_eq!(RenameRule::Camel.apply_to_variant("OpenOnly"), "openOnly");
        assert_eq!(RenameRule::Pascal.apply_to_variant("OpenOnly"), "OpenOnly");
        assert_eq!(
            RenameRule::ScreamingSnake.apply_to_variant("OpenOnly"),
            "OPEN_ONLY"
        );
    }
}
