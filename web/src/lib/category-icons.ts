// Slug -> Iconify icon name (resolved against the fa7-solid collection
// in CategoryIcon.astro). Categories are a closed, developer-controlled
// set, so this mapping lives with the frontend that renders it. To
// switch icon libraries, change the prefix in CategoryIcon.astro and
// adjust any names that differ between collections.
export const ICON_BY_SLUG: Record<string, string> = {
  // Entertainment
  books: "book",
  concerts: "microphone",
  games: "gamepad",
  hobbies: "palette",
  movies: "film",
  music: "music",
  sports: "futbol",
  theater: "masks-theater",

  // Food & drink
  snacks: "cookie-bite",
  dining_out: "utensils",
  liquor: "wine-glass",

  // Home
  groceries: "cart-shopping",
  rent: "house",
  mortgage: "building-columns",
  electronics: "plug",
  furniture: "couch",
  household_supplies: "pump-soap",
  maintenance: "screwdriver-wrench",
  cleaning: "broom",
  pets: "paw",
  services: "bell-concierge",

  // Life
  childcare: "baby",
  clothing: "shirt",
  education: "graduation-cap",
  gifts: "gift",
  insurance: "shield-halved",
  medical: "briefcase-medical",
  taxes: "receipt",
  loan: "hand-holding-dollar",
  hotel: "hotel",
  legal: "scale-balanced",
  real_estate: "building",

  // Transport
  bicycle: "bicycle",
  bus: "bus",
  car: "car",
  fuel: "gas-pump",
  parking: "square-parking",
  plane: "plane",
  taxi: "taxi",
  train: "train",

  // Utilities
  electricity: "bolt",
  heating_gas: "fire",
  internet: "wifi",
  phone: "phone",
  trash: "trash",
  tv: "tv",
  water: "droplet",

  // No category
  other: "thumbtack",
};

export const FALLBACK_ICON = "thumbtack";

export function iconForSlug(slug: string | undefined | null): string {
  return (slug && ICON_BY_SLUG[slug]) || FALLBACK_ICON;
}
