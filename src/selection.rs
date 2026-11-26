// Le texte des bulles, sélectionnable à la souris : glisser, double-clic (un
// mot), triple-clic (toute la bulle), puis Ctrl+C.
//
// La sélection est celle de gpui-base (gpui-component 0.11) : la `Root` de la
// fenêtre dessine un `TextSelectionLayer` qui suit la souris, et répond à
// Ctrl+C (un champ de saisie sans sélection laisse passer l'action jusqu'à
// elle). Un texte y prend part en s'inscrivant à chaque image avec sa mise en
// page (`TextSelectionRun`). Le `SelectableText` de gpui-base ne prend que des
// surlignages (`HighlightStyle`, sans police) : il perdrait la police couleur
// des émojis. D'où cet élément-ci, qui enveloppe le texte tel que Jasette le
// dessine déjà (`StyledText` avec ses morceaux : émojis, passage trouvé,
// liens), ou l'`InteractiveText` des liens cliquables, et n'ajoute que
// l'inscription et le fond de la sélection, peint sous le texte.

use gpui::{
    AnyElement, App, BorderStyle, Bounds, Corners, CursorStyle, Edges, Element, ElementId, GlobalElementId, HitboxBehavior,
    Hsla, InspectorElementId, LayoutId, PaintQuad, Pixels, Point, SharedString, Subscription, TextLayout,
    Window, transparent_black,
};
use gpui_base::{TextSelectionHandle, TextSelectionRegistration, TextSelectionRun};

/// Un texte de bulle qui se sélectionne.
pub(crate) struct TexteSelectionnable {
    id: ElementId,
    texte: SharedString,
    disposition: TextLayout,
    enfant: AnyElement,
    couleur: Hsla,
}

/// `enfant` dessine `texte` (un `StyledText`, ou l'`InteractiveText` qui
/// l'enveloppe) ; `disposition` est la mise en page de ce `StyledText`
/// (`StyledText::layout().clone()`, prise avant de l'envelopper) ; `couleur`,
/// le fond de la sélection.
pub(crate) fn texte_selectionnable(
    id: impl Into<ElementId>,
    texte: impl Into<SharedString>,
    disposition: TextLayout,
    enfant: AnyElement,
    couleur: Hsla,
) -> TexteSelectionnable {
    TexteSelectionnable { id: id.into(), texte: texte.into(), disposition, enfant, couleur }
}

/// Ce que l'élément garde d'une image à l'autre : sa sélection.
struct Retenu {
    poignee: TextSelectionHandle,
    texte: SharedString,
    // Un glissement change la sélection sans image à lui : on en redemande une.
    _rafraichir: Subscription,
}

/// Les rectangles qui couvrent le texte de `debut` à `fin` (lignes pleines entre les deux).
pub(crate) fn rectangles_selection(
    debut: Point<Pixels>,
    fin: Point<Pixels>,
    cadre: Bounds<Pixels>,
    hauteur_ligne: Pixels,
) -> Vec<Bounds<Pixels>> {
    if debut.y == fin.y {
        return vec![Bounds::from_corners(debut, Point::new(fin.x, fin.y + hauteur_ligne))];
    }
    let mut v = vec![Bounds::from_corners(debut, Point::new(cadre.right(), debut.y + hauteur_ligne))];
    if fin.y > debut.y + hauteur_ligne {
        v.push(Bounds::from_corners(Point::new(cadre.left(), debut.y + hauteur_ligne), Point::new(cadre.right(), fin.y)));
    }
    v.push(Bounds::from_corners(Point::new(cadre.left(), fin.y), Point::new(fin.x, fin.y + hauteur_ligne)));
    v
}

impl Element for TexteSelectionnable {
    type RequestLayoutState = TextSelectionHandle;
    type PrepaintState = gpui::Hitbox;

    fn id(&self) -> Option<ElementId> {
        Some(self.id.clone())
    }

    fn source_location(&self) -> Option<&'static std::panic::Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        global_id: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, Self::RequestLayoutState) {
        let texte = self.texte.clone();
        let poignee = window.with_element_state(
            global_id.expect("TexteSelectionnable : un id stable"),
            |retenu: Option<Retenu>, window| {
                let retenu = match retenu {
                    Some(r) => {
                        // Un message modifié : Ctrl+C sans sélection partielle copie le nouveau texte.
                        if r.texte != texte {
                            r.poignee.set_fallback_copy_text(texte.to_string(), cx);
                        }
                        Retenu { texte: texte.clone(), ..r }
                    }
                    None => {
                        let poignee = TextSelectionHandle::new(texte.to_string(), cx);
                        let rafraichir = poignee.refresh_window_on_change(window, cx);
                        Retenu { poignee, texte: texte.clone(), _rafraichir: rafraichir }
                    }
                };
                (retenu.poignee.clone(), retenu)
            },
        );
        (self.enfant.request_layout(window, cx), poignee)
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        poignee: &mut Self::RequestLayoutState,
        window: &mut Window,
        cx: &mut App,
    ) -> Self::PrepaintState {
        self.enfant.prepaint(window, cx);
        // Une zone qui ne bloque rien : les liens, l'appui long et le clic droit
        // de la bulle restent à qui les écoute.
        let zone = window.insert_hitbox(bounds, HitboxBehavior::Normal);
        let inscription = TextSelectionRegistration::new(zone.clone(), bounds)
            .with_text_bounds(vec![bounds])
            .with_rendered_element(poignee, window, cx);
        poignee.register(inscription, window, cx);
        zone
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        poignee: &mut Self::RequestLayoutState,
        zone: &mut Self::PrepaintState,
        window: &mut Window,
        cx: &mut App,
    ) {
        // Le curseur de texte ; celui des liens (main), posé après, l'emporte.
        window.set_cursor_style(CursorStyle::IBeam, zone);
        let d = self.disposition.clone();
        let projection = poignee.update_runs(&[TextSelectionRun::new(self.texte.clone(), d.clone(), bounds)], cx);
        for plage in projection.ranges().iter().flatten().cloned() {
            let (Some(debut), Some(fin)) = (d.position_for_index(plage.start), d.position_for_index(plage.end)) else {
                continue;
            };
            for r in rectangles_selection(debut, fin, d.bounds(), d.line_height()) {
                window.paint_quad(PaintQuad {
                    bounds: r,
                    background: self.couleur.into(),
                    corner_radii: Corners::default(),
                    border_widths: Edges::default(),
                    border_color: transparent_black(),
                    border_style: BorderStyle::default(),
                });
            }
        }
        self.enfant.paint(window, cx);
    }
}
